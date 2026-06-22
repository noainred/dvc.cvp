"""Version reporting and self-upgrade endpoints."""

from __future__ import annotations

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

from ..database import get_db
from ..services import settings as settings_svc
from ..services import system as system_svc

router = APIRouter(prefix="/api/system", tags=["system"])


@router.get("/version")
def version():
    return system_svc.get_version_info()


@router.get("/update-check")
def update_check(fetch: bool = True):
    return system_svc.check_updates(fetch=fetch)


@router.post("/upgrade")
def upgrade(restart: bool = True, db: Session = Depends(get_db)):
    cfg = settings_svc.get(db, "update")
    if not cfg.get("allow_manual", True):
        return {"status": "error", "error": "수동 업그레이드가 비활성화되어 있습니다"}
    return system_svc.perform_upgrade(restart=restart)


@router.get("/upgrade-status")
def upgrade_status():
    return system_svc.get_upgrade_status()
