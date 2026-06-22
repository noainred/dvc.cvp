"""Runtime settings and global event/uptime endpoints."""

from __future__ import annotations

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

from ..database import get_db
from ..schemas import SettingsUpdate
from ..services import settings as settings_svc
from ..services import uptime as uptime_svc

router = APIRouter(prefix="/api", tags=["settings"])

# Keys that must never be exposed verbatim to the browser.
_SENSITIVE = {"synology": ["password"]}


def _redact(data: dict) -> dict:
    out = {}
    for key, value in data.items():
        if key in _SENSITIVE and isinstance(value, dict):
            value = dict(value)
            for field in _SENSITIVE[key]:
                if value.get(field):
                    value[field] = "********"
        out[key] = value
    return out


@router.get("/settings")
def get_settings(db: Session = Depends(get_db)):
    return _redact(settings_svc.get_all(db))


@router.put("/settings")
def update_settings(payload: SettingsUpdate, db: Session = Depends(get_db)):
    data = payload.model_dump(exclude_unset=True)
    for key, patch in data.items():
        if patch is None:
            continue
        # Ignore redacted passwords sent back unchanged.
        if key in _SENSITIVE and isinstance(patch, dict):
            for field in _SENSITIVE[key]:
                if patch.get(field) == "********":
                    patch.pop(field, None)
        settings_svc.update(db, key, patch)
    return _redact(settings_svc.get_all(db))


@router.get("/events")
def events(limit: int = 100, only_down: bool = False, db: Session = Depends(get_db)):
    return uptime_svc.all_events(db, limit=limit, only_down=only_down)
