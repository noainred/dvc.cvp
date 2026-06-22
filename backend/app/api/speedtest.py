"""Internet speed test history and on-demand runs."""

from __future__ import annotations

import datetime as dt
from typing import List, Optional

from fastapi import APIRouter, BackgroundTasks, Depends, Query
from sqlalchemy import func, select
from sqlalchemy.orm import Session

from ..database import get_db
from ..models import SpeedTest
from ..schemas import SpeedTestRead
from ..scheduler import run_speedtest_now
from ..services import settings as settings_svc

router = APIRouter(prefix="/api/speedtest", tags=["speedtest"])


@router.get("", response_model=List[SpeedTestRead])
def history(
    limit: int = Query(200, le=5000),
    days: Optional[int] = None,
    db: Session = Depends(get_db),
):
    stmt = select(SpeedTest)
    if days:
        cutoff = dt.datetime.utcnow() - dt.timedelta(days=days)
        stmt = stmt.where(SpeedTest.ts >= cutoff)
    stmt = stmt.order_by(SpeedTest.ts.desc()).limit(limit)
    rows = db.execute(stmt).scalars().all()
    return list(reversed(rows))


@router.get("/stats")
def stats(days: int = 30, db: Session = Depends(get_db)):
    cutoff = dt.datetime.utcnow() - dt.timedelta(days=days)
    row = db.execute(
        select(
            func.avg(SpeedTest.download_mbps),
            func.min(SpeedTest.download_mbps),
            func.max(SpeedTest.download_mbps),
            func.avg(SpeedTest.upload_mbps),
            func.avg(SpeedTest.ping_ms),
            func.count(),
        ).where(SpeedTest.ts >= cutoff, SpeedTest.ok.is_(True))
    ).one()
    return {
        "days": days,
        "download_avg": round(row[0], 2) if row[0] else None,
        "download_min": round(row[1], 2) if row[1] else None,
        "download_max": round(row[2], 2) if row[2] else None,
        "upload_avg": round(row[3], 2) if row[3] else None,
        "ping_avg": round(row[4], 2) if row[4] else None,
        "count": row[5],
    }


@router.post("/run")
def run_now(background: BackgroundTasks, db: Session = Depends(get_db)):
    """Trigger a speed test in the background and return immediately."""
    cfg = settings_svc.get(db, "speedtest")
    method = cfg.get("method", "auto")
    background.add_task(run_speedtest_now, method)
    return {"status": "started", "method": method}
