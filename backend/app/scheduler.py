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
from .models import Device, SpeedTest
from .monitors import ping as ping_mod
from .monitors import router as router_mod
from .monitors import speedtest as speedtest_mod
from .services import rollup as rollup_svc
from .services import scan as scan_svc
from .services import settings as settings_svc
from .services import synology as synology_svc
from .services import system as system_svc
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
    """Poll every enabled Synology NAS and store a metric for each."""
    with session_scope() as db:
        synology_svc.poll_all(db)


def job_ip_scan() -> None:
    """Scan the configured subnet and record discovered hosts / live state."""
    with session_scope() as db:
        cfg = settings_svc.get(db, "scan")
        if not cfg.get("enabled", True):
            return
        try:
            scan_svc.run_scan(db)
        except Exception as exc:  # pragma: no cover - network dependent
            log.warning("IP scan failed: %s", exc)


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


def job_auto_upgrade() -> None:
    """Periodically check for updates and (optionally) apply them."""
    with session_scope() as db:
        cfg = settings_svc.get(db, "update")
    if not cfg.get("auto_check", True):
        return
    if not system_svc.is_git_repo():
        return
    result = system_svc.check_updates(fetch=True)
    behind = result.get("behind", 0)
    if behind and cfg.get("auto_apply", False):
        log.info("Auto-upgrade: %d commit(s) behind, applying", behind)
        system_svc.perform_upgrade(restart=True)
    elif behind:
        log.info("Update available: %d commit(s) behind upstream", behind)


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
        upd = settings_svc.get(db, "update")
        scn = settings_svc.get(db, "scan")

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

    upd_mins = max(1, int(upd.get("check_interval_minutes", 1)))
    if _current_intervals.get("update") != upd_mins:
        scheduler.reschedule_job("autoupgrade", trigger="interval", minutes=upd_mins)
        _current_intervals["update"] = upd_mins
        log.info("Rescheduled auto-update check to every %dmin", upd_mins)

    scan_mins = max(1, int(scn.get("interval_minutes", 10)))
    if _current_intervals.get("scan") != scan_mins:
        if scn.get("enabled", True):
            scheduler.reschedule_job("ipscan", trigger="interval", minutes=scan_mins)
            try:
                scheduler.resume_job("ipscan")
            except Exception:
                pass
        else:
            try:
                scheduler.pause_job("ipscan")
            except Exception:
                pass
        _current_intervals["scan"] = scan_mins
        log.info("Rescheduled IP scan to every %d min (enabled=%s)", scan_mins, scn.get("enabled", True))


# ---------------------------------------------------------------------------
# Lifecycle
# ---------------------------------------------------------------------------
def start() -> None:
    with session_scope() as db:
        mon = settings_svc.get(db, "monitoring")
        spd = settings_svc.get(db, "speedtest")
        syn = settings_svc.get(db, "synology")
        upd = settings_svc.get(db, "update")
        scn = settings_svc.get(db, "scan")

    monitor_secs = max(10, int(mon.get("interval_seconds", 60)))
    speed_mins = int(spd.get("interval_minutes", 360))
    syn_secs = int(syn.get("poll_seconds", 300))
    upd_mins = max(1, int(upd.get("check_interval_minutes", 1)))
    scan_mins = max(1, int(scn.get("interval_minutes", 10)))

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
    scheduler.add_job(job_auto_upgrade, "interval", minutes=upd_mins, id="autoupgrade",
                      max_instances=1, coalesce=True,
                      next_run_time=dt.datetime.utcnow() + dt.timedelta(minutes=1))
    scheduler.add_job(job_ip_scan, "interval", minutes=scan_mins, id="ipscan",
                      max_instances=1, coalesce=True,
                      next_run_time=dt.datetime.utcnow() + dt.timedelta(seconds=20))
    scheduler.add_job(job_reconcile, "interval", seconds=30, id="reconcile",
                      max_instances=1, coalesce=True)

    if speed_mins <= 0:
        scheduler.pause_job("speedtest")
    if not scn.get("enabled", True):
        scheduler.pause_job("ipscan")

    _current_intervals.update(
        {"monitor": monitor_secs, "speedtest": speed_mins, "update": upd_mins, "scan": scan_mins}
    )
    scheduler.start()
    log.info("Scheduler started (monitor=%ds, speedtest=%dmin, scan=%dmin)", monitor_secs, speed_mins, scan_mins)


def shutdown() -> None:
    if scheduler.running:
        scheduler.shutdown(wait=False)
