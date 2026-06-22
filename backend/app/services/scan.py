"""IP scan orchestration: run a subnet scan, persist the inventory and history."""

from __future__ import annotations

import datetime as dt
import ipaddress
import logging
import time
from typing import Any, Dict, List, Optional

from sqlalchemy import select
from sqlalchemy.orm import Session

from ..models import DiscoveredHost, Device, HostEvent, ScanRun
from ..monitors import scanner
from . import settings as settings_svc

log = logging.getLogger(__name__)


def _utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc).replace(tzinfo=None)


def get_subnet(db: Session) -> str:
    cfg = settings_svc.get(db, "scan")
    return cfg.get("subnet") or scanner.default_subnet()


def _in_subnet(ip: str, subnet: str) -> bool:
    try:
        return ipaddress.ip_address(ip) in ipaddress.ip_network(subnet, strict=False)
    except ValueError:
        return False


def run_scan(db: Session, subnet: Optional[str] = None) -> Dict[str, Any]:
    """Scan the subnet, upsert discovered hosts, and record the run."""
    cfg = settings_svc.get(db, "scan")
    subnet = subnet or cfg.get("subnet") or scanner.default_subnet()
    method = cfg.get("method", "icmp")
    timeout = float(cfg.get("timeout_seconds", 1))
    concurrency = int(cfg.get("concurrency", 64))

    start = time.perf_counter()
    hosts = scanner.scan_subnet(subnet, method=method, timeout=timeout, concurrency=concurrency)
    duration_ms = int((time.perf_counter() - start) * 1000)

    now = _utcnow()
    seen_ips = {h.ip for h in hosts}
    new_count = 0

    for h in hosts:
        row = db.execute(
            select(DiscoveredHost).where(DiscoveredHost.ip == h.ip)
        ).scalar_one_or_none()
        if row is None:
            db.add(
                DiscoveredHost(
                    ip=h.ip,
                    mac=h.mac,
                    hostname=h.hostname,
                    first_seen=now,
                    last_seen=now,
                    last_up=True,
                    times_seen=1,
                )
            )
            db.add(HostEvent(ip=h.ip, ts=now, is_up=True))  # first seen -> up
            new_count += 1
        else:
            if not row.last_up:  # came back online
                db.add(HostEvent(ip=h.ip, ts=now, is_up=True))
            row.last_seen = now
            row.last_up = True
            row.times_seen = (row.times_seen or 0) + 1
            if h.mac:
                row.mac = h.mac
            if h.hostname:
                row.hostname = h.hostname

    # Hosts previously seen in this subnet but missing now -> mark offline.
    for row in db.execute(select(DiscoveredHost)).scalars().all():
        if row.ip not in seen_ips and _in_subnet(row.ip, subnet):
            if row.last_up:  # transition up -> down
                db.add(HostEvent(ip=row.ip, ts=now, is_up=False))
            row.last_up = False

    try:
        total = len(list(ipaddress.ip_network(subnet, strict=False).hosts()))
    except ValueError:
        total = len(hosts)

    run = ScanRun(
        ts=now, subnet=subnet, total=total, up=len(hosts), new=new_count, duration_ms=duration_ms
    )
    db.add(run)
    db.commit()

    log.info("IP scan %s: %d up, %d new (%dms)", subnet, len(hosts), new_count, duration_ms)
    return {
        "subnet": subnet,
        "total": total,
        "up": len(hosts),
        "new": new_count,
        "duration_ms": duration_ms,
        "ts": now.isoformat() + "Z",
    }


def list_hosts(db: Session) -> List[Dict[str, Any]]:
    """Discovered hosts joined with whether they are registered as a device."""
    hosts = db.execute(select(DiscoveredHost).order_by(DiscoveredHost.ip)).scalars().all()
    device_hosts = {d.host: d.id for d in db.execute(select(Device)).scalars()}
    out = []
    for h in hosts:
        out.append(
            {
                "id": h.id,
                "ip": h.ip,
                "mac": h.mac,
                "hostname": h.hostname,
                "vendor": h.vendor,
                "last_up": h.last_up,
                "first_seen": h.first_seen.isoformat() + "Z" if h.first_seen else None,
                "last_seen": h.last_seen.isoformat() + "Z" if h.last_seen else None,
                "times_seen": h.times_seen,
                "device_id": device_hosts.get(h.ip),
            }
        )
    return out


def host_uptime(db: Session, host_id: int) -> Optional[Dict[str, Any]]:
    """How long a discovered host has been online, from scan transition events."""
    host = db.get(DiscoveredHost, host_id)
    if host is None:
        return None
    now = _utcnow()
    events = (
        db.execute(select(HostEvent).where(HostEvent.ip == host.ip).order_by(HostEvent.ts))
        .scalars()
        .all()
    )

    total_online = 0.0
    cur_state: Optional[bool] = None
    last_ts: Optional[dt.datetime] = None
    for e in events:
        if last_ts is not None and cur_state:
            total_online += (e.ts - last_ts).total_seconds()
        cur_state = e.is_up
        last_ts = e.ts

    current_up = 0.0
    if host.last_up:
        if cur_state and last_ts is not None:
            current_up = (now - last_ts).total_seconds()
            total_online += current_up
        elif host.first_seen:  # no events fallback
            current_up = (now - host.first_seen).total_seconds()
            total_online = current_up

    observed = (now - host.first_seen).total_seconds() if host.first_seen else 0.0
    online_pct = round(total_online / observed * 100, 2) if observed > 0 else None

    return {
        "id": host.id,
        "ip": host.ip,
        "mac": host.mac,
        "hostname": host.hostname,
        "last_up": host.last_up,
        "times_seen": host.times_seen,
        "first_seen": host.first_seen.isoformat() + "Z" if host.first_seen else None,
        "last_seen": host.last_seen.isoformat() + "Z" if host.last_seen else None,
        "current_up_seconds": int(current_up),
        "total_online_seconds": int(total_online),
        "observed_seconds": int(observed),
        "online_pct": online_pct,
        "scan_interval_minutes": int(settings_svc.get(db, "scan").get("interval_minutes", 10)),
        "events": [
            {"ts": e.ts.isoformat() + "Z", "is_up": e.is_up}
            for e in sorted(events, key=lambda x: x.ts, reverse=True)[:50]
        ],
    }


def last_run(db: Session) -> Optional[Dict[str, Any]]:
    run = db.execute(select(ScanRun).order_by(ScanRun.ts.desc()).limit(1)).scalar_one_or_none()
    if not run:
        return None
    return {
        "ts": run.ts.isoformat() + "Z",
        "subnet": run.subnet,
        "total": run.total,
        "up": run.up,
        "new": run.new,
        "duration_ms": run.duration_ms,
    }
