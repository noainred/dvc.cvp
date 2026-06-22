"""Recording check results and querying uptime history."""

from __future__ import annotations

import datetime as dt
from typing import Any, Dict, List, Optional

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from ..models import Check, Device, Event, UptimeDaily, UptimeHourly
from ..monitors.ping import CheckResult


def _utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc).replace(tzinfo=None)


def record_result(db: Session, device: Device, result: CheckResult, now: Optional[dt.datetime] = None) -> None:
    """Persist a single check result and update the device's live state.

    A state transition (up<->down) is additionally recorded in the events table
    with the duration of the state that just ended.
    """
    now = now or _utcnow()
    db.add(Check(device_id=device.id, ts=now, is_up=result.is_up, latency_ms=result.latency_ms))

    prev_status = device.last_status
    if prev_status is None or prev_status != result.is_up:
        prev_duration = None
        if device.last_change is not None:
            prev_duration = int((now - device.last_change).total_seconds())
        db.add(
            Event(
                device_id=device.id,
                ts=now,
                is_up=result.is_up,
                prev_duration_s=prev_duration,
            )
        )
        device.last_change = now

    device.last_status = result.is_up
    device.last_latency_ms = result.latency_ms
    device.last_checked = now


# ---------------------------------------------------------------------------
# Queries
# ---------------------------------------------------------------------------
RANGE_SECONDS = {
    "1h": 3600,
    "6h": 6 * 3600,
    "24h": 24 * 3600,
    "7d": 7 * 86400,
    "30d": 30 * 86400,
    "90d": 90 * 86400,
    "1y": 365 * 86400,
    "3y": 3 * 365 * 86400,
}


def _pick_source(seconds: int) -> str:
    """Choose the most appropriate data source for a time range."""
    if seconds <= 24 * 3600:
        return "raw"
    if seconds <= 90 * 86400:
        return "hourly"
    return "daily"


# Explicit granularity (분/시간/날짜) -> data source + default window.
GRAN_SOURCE = {"minute": "raw", "hour": "hourly", "day": "daily"}
GRAN_DEFAULT_RANGE = {"minute": "24h", "hour": "7d", "day": "90d"}


def uptime_summary(
    db: Session,
    device_id: int,
    range_key: str = "24h",
    start: Optional[dt.datetime] = None,
    end: Optional[dt.datetime] = None,
    granularity: Optional[str] = None,
) -> Dict[str, Any]:
    """Return uptime percentage, average latency and a time series.

    ``granularity`` (``minute`` | ``hour`` | ``day``) forces the data source;
    when given without an explicit range it picks a sensible default window.
    """
    now = _utcnow()
    if granularity and not (start and end) and not range_key:
        range_key = GRAN_DEFAULT_RANGE.get(granularity, "24h")

    if start and end:
        seconds = int((end - start).total_seconds())
    else:
        seconds = RANGE_SECONDS.get(range_key or "24h", 86400)
        end = now
        start = now - dt.timedelta(seconds=seconds)

    source = GRAN_SOURCE.get(granularity) if granularity else _pick_source(seconds)

    if source == "raw":
        return _summary_raw(db, device_id, start, end)
    if source == "hourly":
        return _summary_hourly(db, device_id, start, end)
    return _summary_daily(db, device_id, start, end)


def _summary_raw(db: Session, device_id: int, start: dt.datetime, end: dt.datetime) -> Dict[str, Any]:
    rows = db.execute(
        select(Check.ts, Check.is_up, Check.latency_ms)
        .where(Check.device_id == device_id, Check.ts >= start, Check.ts <= end)
        .order_by(Check.ts)
    ).all()
    total = len(rows)
    up = sum(1 for r in rows if r.is_up)
    latencies = [r.latency_ms for r in rows if r.is_up and r.latency_ms is not None]
    series = [
        {"t": r.ts.isoformat() + "Z", "up": bool(r.is_up), "latency": r.latency_ms}
        for r in rows
    ]
    return _build_summary(start, end, "raw", total, up, latencies, series)


def _summary_hourly(db: Session, device_id: int, start: dt.datetime, end: dt.datetime) -> Dict[str, Any]:
    rows = db.execute(
        select(UptimeHourly)
        .where(UptimeHourly.device_id == device_id, UptimeHourly.hour >= start, UptimeHourly.hour <= end)
        .order_by(UptimeHourly.hour)
    ).scalars().all()
    total = sum(r.total for r in rows)
    up = sum(r.up for r in rows)
    latencies = [r.latency_avg for r in rows if r.latency_avg is not None]
    series = [
        {
            "t": r.hour.isoformat() + "Z",
            "uptime": round(r.up / r.total * 100, 2) if r.total else None,
            "latency": r.latency_avg,
        }
        for r in rows
    ]
    return _build_summary(start, end, "hourly", total, up, latencies, series)


def _summary_daily(db: Session, device_id: int, start: dt.datetime, end: dt.datetime) -> Dict[str, Any]:
    rows = db.execute(
        select(UptimeDaily)
        .where(UptimeDaily.device_id == device_id, UptimeDaily.day >= start.date(), UptimeDaily.day <= end.date())
        .order_by(UptimeDaily.day)
    ).scalars().all()
    total = sum(r.total for r in rows)
    up = sum(r.up for r in rows)
    latencies = [r.latency_avg for r in rows if r.latency_avg is not None]
    series = [
        {
            "t": r.day.isoformat(),
            "uptime": round(r.up / r.total * 100, 2) if r.total else None,
            "latency": r.latency_avg,
        }
        for r in rows
    ]
    return _build_summary(start, end, "daily", total, up, latencies, series)


def _build_summary(start, end, source, total, up, latencies, series) -> Dict[str, Any]:
    uptime_pct = round(up / total * 100, 3) if total else None
    latency_avg = round(sum(latencies) / len(latencies), 2) if latencies else None
    return {
        "start": start.isoformat() + "Z",
        "end": end.isoformat() + "Z",
        "source": source,
        "samples": total,
        "up_samples": up,
        "uptime_pct": uptime_pct,
        "latency_avg_ms": latency_avg,
        "series": series,
    }


def recent_events(db: Session, device_id: int, limit: int = 50) -> List[Dict[str, Any]]:
    rows = db.execute(
        select(Event)
        .where(Event.device_id == device_id)
        .order_by(Event.ts.desc())
        .limit(limit)
    ).scalars().all()
    return [
        {
            "ts": r.ts.isoformat() + "Z",
            "is_up": r.is_up,
            "prev_duration_s": r.prev_duration_s,
        }
        for r in rows
    ]


def all_events(db: Session, limit: int = 100, only_down: bool = False) -> List[Dict[str, Any]]:
    stmt = select(Event, Device.name).join(Device, Event.device_id == Device.id)
    if only_down:
        stmt = stmt.where(Event.is_up.is_(False))
    stmt = stmt.order_by(Event.ts.desc()).limit(limit)
    rows = db.execute(stmt).all()
    return [
        {
            "device_id": ev.device_id,
            "device_name": name,
            "ts": ev.ts.isoformat() + "Z",
            "is_up": ev.is_up,
            "prev_duration_s": ev.prev_duration_s,
        }
        for ev, name in rows
    ]
