"""Device CRUD and per-device uptime/history endpoints."""

from __future__ import annotations

import datetime as dt
from typing import List, Optional

from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy import select
from sqlalchemy.orm import Session

from ..database import get_db
from ..models import Device
from ..monitors import ping as ping_mod
from ..schemas import DeviceCreate, DeviceRead, DeviceUpdate
from ..services import uptime as uptime_svc

router = APIRouter(prefix="/api/devices", tags=["devices"])


@router.get("", response_model=List[DeviceRead])
def list_devices(db: Session = Depends(get_db)):
    return db.execute(select(Device).order_by(Device.name)).scalars().all()


@router.post("", response_model=DeviceRead, status_code=201)
def create_device(payload: DeviceCreate, db: Session = Depends(get_db)):
    if payload.check_method == "tcp" and not payload.check_port:
        raise HTTPException(400, "TCP 체크 방식은 포트가 필요합니다")
    device = Device(**payload.model_dump())
    db.add(device)
    db.commit()
    db.refresh(device)
    return device


@router.get("/overview")
def overview(db: Session = Depends(get_db)):
    """Current status of all devices plus today's uptime summary."""
    devices = db.execute(select(Device).order_by(Device.name)).scalars().all()
    out = []
    for d in devices:
        summary = uptime_svc.uptime_summary(db, d.id, "24h")
        out.append(
            {
                "id": d.id,
                "name": d.name,
                "host": d.host,
                "type": d.type,
                "enabled": d.enabled,
                "last_status": d.last_status,
                "last_latency_ms": d.last_latency_ms,
                "last_checked": d.last_checked.isoformat() + "Z" if d.last_checked else None,
                "last_change": d.last_change.isoformat() + "Z" if d.last_change else None,
                "uptime_24h": summary["uptime_pct"],
            }
        )
    total = len(out)
    up = sum(1 for o in out if o["last_status"])
    return {"devices": out, "total": total, "up": up, "down": total - up}


@router.get("/{device_id}", response_model=DeviceRead)
def get_device(device_id: int, db: Session = Depends(get_db)):
    device = db.get(Device, device_id)
    if not device:
        raise HTTPException(404, "장비를 찾을 수 없습니다")
    return device


@router.patch("/{device_id}", response_model=DeviceRead)
def update_device(device_id: int, payload: DeviceUpdate, db: Session = Depends(get_db)):
    device = db.get(Device, device_id)
    if not device:
        raise HTTPException(404, "장비를 찾을 수 없습니다")
    for key, value in payload.model_dump(exclude_unset=True).items():
        setattr(device, key, value)
    db.commit()
    db.refresh(device)
    return device


@router.delete("/{device_id}", status_code=204)
def delete_device(device_id: int, db: Session = Depends(get_db)):
    device = db.get(Device, device_id)
    if not device:
        raise HTTPException(404, "장비를 찾을 수 없습니다")
    db.delete(device)
    db.commit()


@router.get("/{device_id}/uptime")
def device_uptime(
    device_id: int,
    range: str = Query("24h"),
    start: Optional[dt.datetime] = None,
    end: Optional[dt.datetime] = None,
    db: Session = Depends(get_db),
):
    device = db.get(Device, device_id)
    if not device:
        raise HTTPException(404, "장비를 찾을 수 없습니다")
    return uptime_svc.uptime_summary(db, device_id, range, start, end)


@router.get("/{device_id}/events")
def device_events(device_id: int, limit: int = 50, db: Session = Depends(get_db)):
    device = db.get(Device, device_id)
    if not device:
        raise HTTPException(404, "장비를 찾을 수 없습니다")
    return uptime_svc.recent_events(db, device_id, limit)


@router.post("/{device_id}/check")
def check_now(device_id: int, db: Session = Depends(get_db)):
    """Run an immediate one-off check for a device."""
    device = db.get(Device, device_id)
    if not device:
        raise HTTPException(404, "장비를 찾을 수 없습니다")
    result = ping_mod.check(device.host, device.check_method, device.check_port)
    uptime_svc.record_result(db, device, result)
    db.commit()
    return {"is_up": result.is_up, "latency_ms": result.latency_ms, "detail": result.detail}
