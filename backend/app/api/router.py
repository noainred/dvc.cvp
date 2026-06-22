"""Router status endpoint."""

from __future__ import annotations

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

from ..database import get_db
from ..monitors import router as router_mod
from ..services import settings as settings_svc

router = APIRouter(prefix="/api/router", tags=["router"])


@router.get("/status")
def status(db: Session = Depends(get_db)):
    cfg = settings_svc.get(db, "router")
    if not cfg.get("enabled"):
        return {"enabled": False}
    data = router_mod.get_status(cfg)
    data["enabled"] = True
    return data
