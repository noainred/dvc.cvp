"""OS-level security (fail2ban) control endpoints."""

from __future__ import annotations

from fastapi import APIRouter, Depends
from pydantic import BaseModel
from sqlalchemy.orm import Session

from ..database import get_db
from ..monitors import fail2ban as f2b
from ..services import settings as settings_svc

router = APIRouter(prefix="/api/security", tags=["security"])


class F2BPolicy(BaseModel):
    enabled: bool | None = None
    jail: str | None = None
    maxretry: int | None = None
    bantime_minutes: int | None = None
    findtime_minutes: int | None = None


@router.get("/fail2ban/status")
def fail2ban_status(db: Session = Depends(get_db)):
    cfg = settings_svc.get(db, "fail2ban")
    out = {"config": cfg, "installed": f2b.is_installed()}
    if f2b.is_installed():
        out["status"] = f2b.status(cfg.get("jail", "sshd"))
    return out


@router.put("/fail2ban/policy")
def fail2ban_policy(payload: F2BPolicy, db: Session = Depends(get_db)):
    patch = payload.model_dump(exclude_unset=True)
    cfg = settings_svc.update(db, "fail2ban", patch)
    result = f2b.apply_policy(
        jail=cfg.get("jail", "sshd"),
        maxretry=int(cfg.get("maxretry", 3)),
        bantime_minutes=int(cfg.get("bantime_minutes", 15)),
        findtime_minutes=int(cfg.get("findtime_minutes", 10)),
    )
    return {"config": cfg, "apply": result}


@router.post("/fail2ban/unban/{ip}")
def fail2ban_unban(ip: str, db: Session = Depends(get_db)):
    cfg = settings_svc.get(db, "fail2ban")
    return f2b.unban(cfg.get("jail", "sshd"), ip)
