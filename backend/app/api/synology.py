"""Synology NAS management (multiple units) and status/history."""

from __future__ import annotations

import json
from typing import List, Optional

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from sqlalchemy.orm import Session

from ..database import get_db
from ..models import SynologyNas
from ..services import synology as syno_svc

router = APIRouter(prefix="/api/synology", tags=["synology"])


class NasCreate(BaseModel):
    name: str
    host: str
    port: int = 5001
    https: bool = True
    verify_ssl: bool = False
    username: str = ""
    password: str = ""
    otp_code: Optional[str] = None
    enabled: bool = True


class NasUpdate(BaseModel):
    name: Optional[str] = None
    host: Optional[str] = None
    port: Optional[int] = None
    https: Optional[bool] = None
    verify_ssl: Optional[bool] = None
    username: Optional[str] = None
    password: Optional[str] = None
    otp_code: Optional[str] = None
    enabled: Optional[bool] = None


@router.get("/nas")
def list_nas(db: Session = Depends(get_db)):
    return [syno_svc.public_dict(n) for n in syno_svc.list_nas(db)]


@router.post("/nas", status_code=201)
def create_nas(payload: NasCreate, db: Session = Depends(get_db)):
    nas = SynologyNas(**payload.model_dump())
    db.add(nas)
    db.commit()
    db.refresh(nas)
    return syno_svc.public_dict(nas)


@router.patch("/nas/{nas_id}")
def update_nas(nas_id: int, payload: NasUpdate, db: Session = Depends(get_db)):
    nas = db.get(SynologyNas, nas_id)
    if not nas:
        raise HTTPException(404, "NAS를 찾을 수 없습니다")
    data = payload.model_dump(exclude_unset=True)
    # Ignore the masked password sent back unchanged.
    if data.get("password") == "********":
        data.pop("password", None)
    # Changing connection identity invalidates the trusted-device token.
    if any(k in data for k in ("host", "username")):
        nas.device_id = None
    for key, value in data.items():
        setattr(nas, key, value)
    db.commit()
    db.refresh(nas)
    return syno_svc.public_dict(nas)


@router.delete("/nas/{nas_id}", status_code=204)
def delete_nas(nas_id: int, db: Session = Depends(get_db)):
    nas = db.get(SynologyNas, nas_id)
    if not nas:
        raise HTTPException(404, "NAS를 찾을 수 없습니다")
    db.delete(nas)
    db.commit()


@router.get("/nas/{nas_id}/status")
def nas_status(nas_id: int, db: Session = Depends(get_db)):
    nas = db.get(SynologyNas, nas_id)
    if not nas:
        raise HTTPException(404, "NAS를 찾을 수 없습니다")
    status = syno_svc.poll_nas(db, nas, store_metric=True)
    status["nas_id"] = nas_id
    status["name"] = nas.name
    return status


@router.get("/nas/{nas_id}/history")
def nas_history(nas_id: int, hours: int = 24, db: Session = Depends(get_db)):
    nas = db.get(SynologyNas, nas_id)
    if not nas:
        raise HTTPException(404, "NAS를 찾을 수 없습니다")
    return syno_svc.history(db, nas_id, hours=hours)


@router.get("/overview")
def overview(db: Session = Depends(get_db)):
    """All NAS with their last known live status (no network calls)."""
    out = []
    for nas in syno_svc.list_nas(db):
        info = syno_svc.public_dict(nas)
        info["detail"] = json.loads(nas.last_detail) if nas.last_detail else None
        out.append(info)
    return out
