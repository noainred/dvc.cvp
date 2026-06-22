"""Multi-NAS Synology management: CRUD, polling and status."""

from __future__ import annotations

import datetime as dt
import json
import logging
from typing import Any, Dict, List, Optional

from sqlalchemy import select
from sqlalchemy.orm import Session

from ..models import SynologyMetric, SynologyNas
from ..monitors import synology as syno_mon

log = logging.getLogger(__name__)


def _utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc).replace(tzinfo=None)


def nas_to_cfg(nas: SynologyNas) -> Dict[str, Any]:
    return {
        "host": nas.host,
        "port": nas.port,
        "https": nas.https,
        "verify_ssl": nas.verify_ssl,
        "username": nas.username,
        "password": nas.password,
        "otp_code": nas.otp_code,
        "device_id": nas.device_id,
    }


def public_dict(nas: SynologyNas, redact: bool = True) -> Dict[str, Any]:
    return {
        "id": nas.id,
        "name": nas.name,
        "host": nas.host,
        "port": nas.port,
        "https": nas.https,
        "verify_ssl": nas.verify_ssl,
        "username": nas.username,
        "password": "********" if (redact and nas.password) else nas.password,
        "has_otp": bool(nas.otp_code),
        "has_device_token": bool(nas.device_id),
        "enabled": nas.enabled,
        "last_connected": nas.last_connected,
        "last_polled": nas.last_polled.isoformat() + "Z" if nas.last_polled else None,
    }


def list_nas(db: Session) -> List[SynologyNas]:
    return db.execute(select(SynologyNas).order_by(SynologyNas.name)).scalars().all()


def poll_nas(db: Session, nas: SynologyNas, store_metric: bool = True) -> Dict[str, Any]:
    """Connect to one NAS, update its live state and optionally store a metric.

    Returns the status dict (with a friendly ``error`` key on failure).
    """
    try:
        status = syno_mon.get_status(nas_to_cfg(nas))
    except syno_mon.SynologyError as exc:
        nas.last_connected = False
        nas.last_polled = _utcnow()
        db.commit()
        return {"connected": False, "error": str(exc), "otp_required": getattr(exc, "otp_required", False)}
    except Exception as exc:  # pragma: no cover - network dependent
        nas.last_connected = False
        nas.last_polled = _utcnow()
        db.commit()
        return {"connected": False, "error": str(exc)}

    # Persist a freshly issued trusted-device token and consume the one-time OTP.
    if status.get("device_id"):
        nas.device_id = status["device_id"]
        nas.otp_code = None
        log.info("NAS '%s': trusted device token registered", nas.name)

    nas.last_connected = bool(status.get("connected"))
    nas.last_polled = _utcnow()
    nas.last_detail = json.dumps(status, default=str)

    if store_metric and status.get("connected"):
        db.add(
            SynologyMetric(
                nas_id=nas.id,
                cpu_load=status.get("cpu_load"),
                mem_usage=status.get("mem_usage"),
                temp_c=status.get("temperature_c"),
                uptime_s=status.get("uptime_s"),
                detail=json.dumps({"volumes": status.get("volumes", [])}),
            )
        )
    db.commit()
    return status


def poll_all(db: Session) -> None:
    for nas in db.execute(select(SynologyNas).where(SynologyNas.enabled.is_(True))).scalars().all():
        poll_nas(db, nas, store_metric=True)


def history(db: Session, nas_id: int, hours: int = 24, limit: int = 500) -> List[Dict[str, Any]]:
    cutoff = dt.datetime.utcnow() - dt.timedelta(hours=hours)
    rows = (
        db.execute(
            select(SynologyMetric)
            .where(SynologyMetric.nas_id == nas_id, SynologyMetric.ts >= cutoff)
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
            }
        )
    return out
