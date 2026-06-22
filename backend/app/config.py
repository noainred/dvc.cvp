"""Bootstrap configuration loading.

System-level settings (DB path, server host/port) come from ``config.yaml`` and
environment variables and require a restart to change. Runtime settings
(intervals, retention, Synology/router credentials) are seeded from this file on
first run but are afterwards stored in the database and editable from the web UI
(see :mod:`backend.app.services.settings`).
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any, Dict

import yaml

# Repository root (two levels up from this file: backend/app/config.py -> repo/)
ROOT_DIR = Path(__file__).resolve().parents[2]

DEFAULTS: Dict[str, Any] = {
    "server": {"host": "0.0.0.0", "port": 8080},
    "database": {"path": "./data/homelab.db"},
    "log_level": "INFO",
    "monitoring": {"interval_seconds": 60, "concurrency": 16, "timeout_seconds": 2},
    "retention": {"raw_days": 90, "hourly_days": 400},
    "speedtest": {"enabled": True, "interval_minutes": 360, "method": "auto"},
    "scan": {
        "enabled": True,
        "subnet": "",  # 빈 값이면 라즈베리파이의 로컬 /24 자동 감지
        "interval_minutes": 10,
        "method": "icmp",
        "timeout_seconds": 1,
        "concurrency": 64,
    },
    "update": {
        "auto_check": True,
        "check_interval_minutes": 1,
        "auto_apply": False,
        "allow_manual": True,
    },
    "security": {
        "auth_enabled": False,   # 로그인(TOTP) 요구 여부
        "max_attempts": 3,       # 이 횟수 이상 실패하면 IP 차단
        "ban_minutes": 15,       # 차단 지속 시간(분), 0이면 영구
        "window_minutes": 10,    # 실패 횟수를 세는 시간 창(분)
    },
    "fail2ban": {
        "enabled": False,        # 라즈베리파이 OS(SSH 등) fail2ban 제어
        "jail": "sshd",
        "maxretry": 3,
        "bantime_minutes": 15,
        "findtime_minutes": 10,
    },
    # Per-NAS connection details live in the synology_nas table (multi-NAS).
    # This only holds the global polling interval.
    "synology": {"poll_seconds": 300},
    # Initial NAS units to seed on first run (afterwards managed in the web UI).
    "synology_nas": [],
    "tailscale": {"enabled": True},
    "router": {
        "enabled": False,
        "host": "",
        "admin_url": "",
        "snmp_enabled": False,
        "snmp_community": "public",
        "snmp_port": 161,
        "wan_if_index": None,
    },
    "devices": [],
}


def _deep_merge(base: Dict[str, Any], override: Dict[str, Any]) -> Dict[str, Any]:
    out = dict(base)
    for key, value in (override or {}).items():
        if isinstance(value, dict) and isinstance(out.get(key), dict):
            out[key] = _deep_merge(out[key], value)
        else:
            out[key] = value
    return out


class Config:
    """Loaded application configuration with convenient accessors."""

    def __init__(self, data: Dict[str, Any], path: Path | None):
        self._data = data
        self.path = path

    # --- raw access ---------------------------------------------------------
    def __getitem__(self, key: str) -> Any:
        return self._data[key]

    def get(self, key: str, default: Any = None) -> Any:
        return self._data.get(key, default)

    @property
    def data(self) -> Dict[str, Any]:
        return self._data

    # --- system settings ----------------------------------------------------
    @property
    def host(self) -> str:
        return os.getenv("HOMELAB_HOST", self._data["server"]["host"])

    @property
    def port(self) -> int:
        return int(os.getenv("HOMELAB_PORT", self._data["server"]["port"]))

    @property
    def log_level(self) -> str:
        return os.getenv("HOMELAB_LOG_LEVEL", self._data["log_level"]).upper()

    @property
    def db_path(self) -> Path:
        raw = os.getenv("HOMELAB_DB", self._data["database"]["path"])
        p = Path(raw)
        if not p.is_absolute():
            p = (ROOT_DIR / p).resolve()
        return p

    @property
    def db_url(self) -> str:
        return f"sqlite:///{self.db_path}"


def load_config() -> Config:
    """Load configuration from (in priority order) env-specified path,
    ./config.yaml, or built-in defaults."""
    candidates = []
    env_path = os.getenv("HOMELAB_CONFIG")
    if env_path:
        candidates.append(Path(env_path))
    candidates.append(ROOT_DIR / "config.yaml")
    candidates.append(ROOT_DIR / "config.local.yaml")

    data = dict(DEFAULTS)
    used: Path | None = None
    for candidate in candidates:
        if candidate and candidate.is_file():
            with open(candidate, "r", encoding="utf-8") as fh:
                loaded = yaml.safe_load(fh) or {}
            data = _deep_merge(data, loaded)
            used = candidate
            break

    return Config(data, used)


# Module-level singleton
config = load_config()
