"""Runtime, web-editable settings stored in the database.

Settings are seeded from ``config.yaml`` on first run and afterwards managed via
the web UI. Reads are cheap (a single keyed row) so the scheduler can call them
on every cycle and pick up changes without a restart.
"""

from __future__ import annotations

import json
from typing import Any, Dict

from sqlalchemy.orm import Session

from ..config import config
from ..models import Setting

# Keys persisted to the DB and editable from the web UI, with their seed source
# inside config.yaml.
_SEED_MAP = {
    "monitoring": "monitoring",
    "retention": "retention",
    "speedtest": "speedtest",
    "scan": "scan",
    "synology": "synology",
    "router": "router",
    "update": "update",
    "tailscale": "tailscale",
    "security": "security",
    "fail2ban": "fail2ban",
}


def seed_defaults(db: Session) -> None:
    """Insert any missing settings rows from the bootstrap config."""
    changed = False
    for key, cfg_key in _SEED_MAP.items():
        existing = db.get(Setting, key)
        if existing is None:
            value = config.get(cfg_key, {})
            db.add(Setting(key=key, value=json.dumps(value)))
            changed = True
    if changed:
        db.commit()


def get(db: Session, key: str, default: Any = None) -> Any:
    row = db.get(Setting, key)
    if row is None:
        return default if default is not None else config.get(key, {})
    return json.loads(row.value)


def get_all(db: Session) -> Dict[str, Any]:
    return {key: get(db, key) for key in _SEED_MAP}


def set(db: Session, key: str, value: Any) -> None:
    row = db.get(Setting, key)
    payload = json.dumps(value)
    if row is None:
        db.add(Setting(key=key, value=payload))
    else:
        row.value = payload
    db.commit()


def update(db: Session, key: str, patch: Dict[str, Any]) -> Dict[str, Any]:
    """Shallow-merge ``patch`` into an existing dict-valued setting."""
    current = get(db, key)
    if not isinstance(current, dict):
        current = {}
    current.update(patch or {})
    set(db, key, current)
    return current
