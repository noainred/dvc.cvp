"""Background scheduler wiring all periodic jobs together.

Uses APScheduler's BackgroundScheduler. Intervals are runtime-editable (stored
in the DB); a lightweight ``reconcile`` job re-reads them periodically and
reschedules the affected jobs so web edits take effect without a restart.
"""

from __future__ import annotations

import datetime as dt
import logging
from concurrent.futures import ThreadPoolExecutor
from typing import Dict

from apscheduler.schedulers.background import BackgroundScheduler
from sqlalchemy import select

from .database import session_scope
from .models import Device, SpeedTest, SynologyMetric
from .monitors import ping as ping_mod
from .monitors import router as router_mod
from .monitors import speedtest as speedtest_mod
from .monitors import synology as synology_mod
from .services import rollup as rollup_svc
from .services import settings as settings_svc
from .services import uptime as uptime_svc

log = logging.getLogger(__name__)

scheduler = BackgroundScheduler(timezone="UTC")

# Remember the intervals jobs were last scheduled with, to detect changes.
_current_intervals: Dict[str, int] = {}

# Guard so on-demand and scheduled speed tests never overlap.
_speedtest_running = False


# ---------------------------------------------------------------------------
# Jobs
# ---------------------------------------------------------------------------
def job_monitor() -> None:
    """Ping every enabled device concurrently and record the results."""
    with session_scope() as db:
        mon = settings_svc.get(db, "monitoring")
        timeout = float(mon.get("timeout_seconds", 2))
        concurrency = int(mon.get("concurrency", 16))
        specs = [
            (d.id, d.host, d.check_method, d.check_port)
            for d in db.execute(select(Device).where(Device.enabled.is_(True))).scalars()
        ]

    if not specs:
        return

    def _run(spec):
        dev_id, host, method, port = spec
        return dev_id, ping_mod.check(host, method, port, timeout)

    results = {}
    workers = max(1, min(concurrency, len(specs)))
    with ThreadPoolExecutor(max_workers=workers) as pool:
        for dev_id, result in pool.map(_run, specs):
            results[dev_id] = result

    now = dt.datetime.utcnow()
    with session_scope() as db:
        for dev_id, result in results.items():
            device = db.get(Device, dev_id)
            if device is not None:
                uptime_svc.record_result(db, device, result, now)


def job_speedtest() -> None:
    global _speedtest_running
    with session_scope() as db:
        cfg = settings_svc.get(db, "speedtest")
    if not cfg.get("enabled", True):
        return
    run_speedtest_now(cfg.get("method", "auto"))


def run_speedtest_now(method: str = "auto") -> dict:
    """Run a speed test immediately and persist the result."""
    global _speedtest_running
    if _speedtest_running:
        return {"ok": False, "error": "speedtest already running"}
    _speedtest_running = True
    try:
        result = speedtest_mod.run_speedtest(method)
    finally:
        _speedtest_running = False

    with session_scope() as db:
        db.add(
            SpeedTest(
                ok=result.ok,
                download_mbps=result.download_mbps,
                upload_mbps=result.upload_mbps,
                ping_ms=result.ping_ms,
                jitter_ms=result.jitter_ms,
                server=result.server,
                isp=result.isp,
                error=result.error,
            )
        )
    return result.as_dict()


def job_synology() -> None:
    import json

    with session_scope() as db:
        cfg = settings_svc.get(db, "synology")
    if not cfg.get("enabled") or not cfg.get("host"):
        return
    try:
        status = synology_mod.get_status(cfg)
    except Exception as exc:  # pragma: no cover - network dependent
        log.warning("Synology poll failed: %s", exc)
        return
    if not status.get("connected"):
        return
    with session_scope() as db:
        db.add(
            SynologyMetric(
                cpu_load=status.get("cpu_load"),
                mem_usage=status.get("mem_usage"),
                temp_c=status.get("temperature_c"),
                uptime_s=status.get("uptime_s"),
                detail=json.dumps({"volumes": status.get("volumes", [])}),
            )
        )


def job_rollup() -> None:
    with session_scope() as db:
        rollup_svc.run_rollup(db)


def job_prune() -> None:
    with session_scope() as db:
        ret = settings_svc.get(db, "retention")
        rollup_svc.prune(
            db,
            raw_days=int(ret.get("raw_days", 90)),
            hourly_days=int(ret.get("hourly_days", 400)),
        )


def job_router_poll() -> None:
    """Keep WAN throughput counters warm so the router page shows live rates."""
    with session_scope() as db:
        cfg = settings_svc.get(db, "router")
    if not cfg.get("enabled") or not cfg.get("snmp_enabled") or not cfg.get("host"):
        return
    try:
        router_mod.get_status(cfg)
    except Exception:  # pragma: no cover
        pass


# ---------------------------------------------------------------------------
# Reconcile (apply runtime interval changes)
# ---------------------------------------------------------------------------
def job_reconcile() -> None:
    with session_scope() as db:
        mon = settings_svc.get(db, "monitoring")
        spd = settings_svc.get(db, "speedtest")

    monitor_secs = max(10, int(mon.get("interval_seconds", 60)))
    if _current_intervals.get("monitor") != monitor_secs:
        scheduler.reschedule_job("monitor", trigger="interval", seconds=monitor_secs)
        _current_intervals["monitor"] = monitor_secs
        log.info("Rescheduled monitor job to every %ds", monitor_secs)

    speed_mins = int(spd.get("interval_minutes", 360))
    if _current_intervals.get("speedtest") != speed_mins:
        if speed_mins > 0:
            scheduler.reschedule_job("speedtest", trigger="interval", minutes=speed_mins)
        else:
            try:
                scheduler.pause_job("speedtest")
            except Exception:
                pass
        _current_intervals["speedtest"] = speed_mins
        log.info("Rescheduled speedtest job to every %d min", speed_mins)


# ---------------------------------------------------------------------------
# Lifecycle
# ---------------------------------------------------------------------------
def start() -> None:
    with session_scope() as db:
        mon = settings_svc.get(db, "monitoring")
        spd = settings_svc.get(db, "speedtest")
        syn = settings_svc.get(db, "synology")

    monitor_secs = max(10, int(mon.get("interval_seconds", 60)))
    speed_mins = int(spd.get("interval_minutes", 360))
    syn_secs = int(syn.get("poll_seconds", 300))

    scheduler.add_job(job_monitor, "interval", seconds=monitor_secs, id="monitor",
                      max_instances=1, coalesce=True, next_run_time=dt.datetime.utcnow())
    scheduler.add_job(job_speedtest, "interval", minutes=max(1, speed_mins), id="speedtest",
                      max_instances=1, coalesce=True)
    scheduler.add_job(job_synology, "interval", seconds=max(60, syn_secs), id="synology",
                      max_instances=1, coalesce=True)
    scheduler.add_job(job_router_poll, "interval", seconds=30, id="router",
                      max_instances=1, coalesce=True)
    scheduler.add_job(job_rollup, "interval", minutes=10, id="rollup",
                      max_instances=1, coalesce=True)
    scheduler.add_job(job_prune, "cron", hour=4, minute=15, id="prune", max_instances=1)
    scheduler.add_job(job_reconcile, "interval", seconds=30, id="reconcile",
                      max_instances=1, coalesce=True)

    if speed_mins <= 0:
        scheduler.pause_job("speedtest")

    _current_intervals.update({"monitor": monitor_secs, "speedtest": speed_mins})
    scheduler.start()
    log.info("Scheduler started (monitor=%ds, speedtest=%dmin)", monitor_secs, speed_mins)


def shutdown() -> None:
    if scheduler.running:
        scheduler.shutdown(wait=False)
