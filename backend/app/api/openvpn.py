"""OpenVPN server status and management endpoints."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import FileResponse
from pydantic import BaseModel
from sqlalchemy.orm import Session

from ..database import get_db
from ..monitors import openvpn as ovpn
from ..services import settings as settings_svc

router = APIRouter(prefix="/api/openvpn", tags=["openvpn"])


class ClientIn(BaseModel):
    name: str


@router.get("/status")
def status(db: Session = Depends(get_db)):
    return ovpn.get_status(settings_svc.get(db, "openvpn"))


@router.get("/profiles/{name}/download")
def download(name: str, db: Session = Depends(get_db)):
    cfg = settings_svc.get(db, "openvpn")
    path = ovpn.find_profile(name, cfg.get("ovpn_dirs", ["~", "/root", "/etc/openvpn"]))
    if not path:
        raise HTTPException(404, "프로필(.ovpn)을 찾을 수 없습니다")
    return FileResponse(path, filename=f"{name}.ovpn", media_type="application/x-openvpn-profile")


@router.post("/install")
def install():
    return ovpn.run_action("install")


@router.post("/clients")
def add_client(payload: ClientIn):
    return ovpn.run_action("add", payload.name.strip())


@router.post("/clients/{name}/revoke")
def revoke_client(name: str):
    return ovpn.run_action("revoke", name.strip())


@router.get("/action")
def action_status():
    return ovpn.action_state()
