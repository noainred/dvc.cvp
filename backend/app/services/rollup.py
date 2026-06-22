"""Aggregation (rollup) and retention pruning.

Runs hourly. Raw checks are aggregated into hourly buckets, hourly buckets into
daily buckets. Daily buckets and events are kept forever, giving multi-year
queryability while the raw (high-volume) table stays small.

A watermark (stored in the settings table) tracks the last fully-processed hour
so each run only does incremental work and is safe to run repeatedly.
"""

from __future__ import annotations

import datetime as dt
import logging
from typing import Optional

from sqlalchemy import Integer, cast, delete, func, select
from sqlalchemy.orm import Session

from ..models import Check, UptimeDaily, UptimeHourly
from . import settings as settings_svc

log = logging.getLogger(__name__)

_WATERMARK_KEY = "rollup_watermark"
_MAX_HOURS_PER_RUN = 24 * 120  # safety cap (~120 days backfill in one run)


def _floor_hour(ts: dt.datetime) -> dt.datetime:
    return ts.replace(minute=0, second=0, microsecond=0)


def _get_watermark(db: Session, default: dt.datetime) -> dt.datetime:
    raw = settings_svc.get(db, _WATERMARK_KEY, None)
    if isinstance(raw, str):
        try:
            return dt.datetime.fromisoformat(raw)
        except ValueError:
            pass
    return default


def _set_watermark(db: Session, hour: dt.datetime) -> None:
    settings_svc.set(db, _WATERMARK_KEY, hour.isoformat())


def _aggregate_hour(db: Session, hour: dt.datetime) -> None:
    nxt = hour + dt.timedelta(hours=1)
    rows = db.execute(
        select(
            Check.device_id,
            func.count().label("total"),
            func.sum(cast(Check.is_up, Integer)).label("up"),
        )
        .where(Check.ts >= hour, Check.ts < nxt)
        .group_by(Check.device_id)
    ).all()

    for device_id, total, up in rows:
        up = int(up or 0)
        # Latency stats over successful checks only.
        lat = db.execute(
            select(
                func.avg(Check.latency_ms),
                func.min(Check.latency_ms),
                func.max(Check.latency_ms),
            ).where(
                Check.device_id == device_id,
                Check.ts >= hour,
                Check.ts < nxt,
                Check.is_up.is_(True),
                Check.latency_ms.is_not(None),
            )
        ).one()
        avg_l = round(lat[0], 2) if lat[0] is not None else None
        db.merge(
            UptimeHourly(
                device_id=device_id,
                hour=hour,
                total=int(total),
                up=up,
                latency_avg=avg_l,
                latency_min=lat[1],
                latency_max=lat[2],
            )
        )


def _aggregate_day(db: Session, day: dt.date) -> None:
    start = dt.datetime.combine(day, dt.time.min)
    end = start + dt.timedelta(days=1)
    rows = db.execute(
        select(
            UptimeHourly.device_id,
            func.sum(UptimeHourly.total),
            func.sum(UptimeHourly.up),
            func.avg(UptimeHourly.latency_avg),
            func.min(UptimeHourly.latency_min),
            func.max(UptimeHourly.latency_max),
        )
        .where(UptimeHourly.hour >= start, UptimeHourly.hour < end)
        .group_by(UptimeHourly.device_id)
    ).all()

    for device_id, total, up, avg_l, min_l, max_l in rows:
        db.merge(
            UptimeDaily(
                device_id=device_id,
                day=day,
                total=int(total or 0),
                up=int(up or 0),
                latency_avg=round(avg_l, 2) if avg_l is not None else None,
                latency_min=min_l,
                latency_max=max_l,
            )
        )


def run_rollup(db: Session, now: Optional[dt.datetime] = None) -> int:
    """Process all complete hours since the watermark. Returns hours processed."""
    now = now or dt.datetime.utcnow()
    current_hour = _floor_hour(now)

    # Default watermark: the hour of the earliest raw check, else last hour.
    earliest = db.execute(select(func.min(Check.ts))).scalar()
    default_wm = _floor_hour(earliest) - dt.timedelta(hours=1) if earliest else current_hour - dt.timedelta(hours=1)
    watermark = _get_watermark(db, default_wm)

    hour = watermark + dt.timedelta(hours=1)
    processed = 0
    days_touched: set[dt.date] = set()
    while hour < current_hour and processed < _MAX_HOURS_PER_RUN:
        _aggregate_hour(db, hour)
        days_touched.add(hour.date())
        watermark = hour
        hour += dt.timedelta(hours=1)
        processed += 1

    # Flush the merged hourly rows so the daily aggregation below can read them
    # (the session uses autoflush=False).
    if days_touched:
        db.flush()
    for day in sorted(days_touched):
        _aggregate_day(db, day)

    if processed:
        _set_watermark(db, watermark)
        db.commit()
        log.info("Rollup processed %d hour(s), updated %d day(s)", processed, len(days_touched))
    return processed


def prune(db: Session, raw_days: int, hourly_days: int, now: Optional[dt.datetime] = None) -> dict:
    """Delete data older than the configured retention windows."""
    now = now or dt.datetime.utcnow()
    raw_cutoff = now - dt.timedelta(days=raw_days)
    hourly_cutoff = now - dt.timedelta(days=hourly_days)

    raw_deleted = db.execute(delete(Check).where(Check.ts < raw_cutoff)).rowcount
    hourly_deleted = db.execute(
        delete(UptimeHourly).where(UptimeHourly.hour < hourly_cutoff)
    ).rowcount
    db.commit()
    if raw_deleted or hourly_deleted:
        log.info("Pruned %d raw / %d hourly rows", raw_deleted or 0, hourly_deleted or 0)
    return {"raw_deleted": raw_deleted or 0, "hourly_deleted": hourly_deleted or 0}
