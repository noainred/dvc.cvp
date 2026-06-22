"""Synology NAS status and metric history."""

from __future__ import annotations

import datetime as dt
import json
from typing import List

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import select
from sqlalchemy.orm import Session

from ..database import get_db
from ..models import SynologyMetric
from ..monitors import synology as synology_mod
from ..services import settings as settings_svc

router = APIRouter(prefix="/api/synology", tags=["synology"])


@router.get("/status")
def status(db: Session = Depends(get_db)):
    cfg = settings_svc.get(db, "synology")
    if not cfg.get("enabled"):
        return {"enabled": False}
    if not cfg.get("host"):
        raise HTTPException(400, "시놀로지 호스트가 설정되지 않았습니다")
    try:
        data = synology_mod.get_status(cfg)
    except Exception as exc:
        raise HTTPException(502, f"시놀로지 연결 실패: {exc}")
    data["enabled"] = True
    return data


@router.get("/history")
def history(hours: int = 24, limit: int = 500, db: Session = Depends(get_db)):
    cutoff = dt.datetime.utcnow() - dt.timedelta(hours=hours)
    rows = (
        db.execute(
            select(SynologyMetric)
            .where(SynologyMetric.ts >= cutoff)
            .order_by(SynologyMetric.ts.desc())
            .limit(limit)
        )
        .scalars()
        .all()
    )
    out = []
    for r in reversed(rows):
        out.append(
            {
                "ts": r.ts.isoformat() + "Z",
                "cpu_load": r.cpu_load,
                "mem_usage": r.mem_usage,
                "temp_c": r.temp_c,
                "uptime_s": r.uptime_s,
                "detail": json.loads(r.detail) if r.detail else None,
            }
        )
    return out
