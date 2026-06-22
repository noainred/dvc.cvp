"""Minimal Synology DSM Web API client.

Authenticates against DSM and pulls system information, resource utilisation and
storage/volume status. Designed to fail soft: any sub-call that errors simply
omits its fields rather than failing the whole status fetch.

DSM API reference: ``/webapi/auth.cgi`` (login) and ``/webapi/entry.cgi``.
The account used should have admin rights and ideally 2-factor auth disabled
(or use an app-specific account).
"""

from __future__ import annotations

import logging
from typing import Any, Dict, Optional

import httpx

log = logging.getLogger(__name__)


class SynologyError(Exception):
    pass


class SynologyClient:
    def __init__(self, cfg: Dict[str, Any]):
        scheme = "https" if cfg.get("https", True) else "http"
        self.base = f"{scheme}://{cfg['host']}:{cfg.get('port', 5001)}/webapi"
        self.username = cfg.get("username", "")
        self.password = cfg.get("password", "")
        self.verify = bool(cfg.get("verify_ssl", False))
        self._sid: Optional[str] = None
        self._client = httpx.Client(verify=self.verify, timeout=15.0)

    # --- low level ----------------------------------------------------------
    def _get(self, path: str, params: Dict[str, Any]) -> Dict[str, Any]:
        resp = self._client.get(f"{self.base}/{path}", params=params)
        resp.raise_for_status()
        data = resp.json()
        if not data.get("success"):
            raise SynologyError(f"API error: {data.get('error')}")
        return data.get("data", {})

    def login(self) -> None:
        params = {
            "api": "SYNO.API.Auth",
            "version": 6,
            "method": "login",
            "account": self.username,
            "passwd": self.password,
            "session": "HomeLabMonitor",
            "format": "sid",
        }
        data = self._get("auth.cgi", params)
        self._sid = data.get("sid")
        if not self._sid:
            raise SynologyError("login returned no sid")

    def logout(self) -> None:
        if not self._sid:
            return
        try:
            self._get(
                "auth.cgi",
                {
                    "api": "SYNO.API.Auth",
                    "version": 6,
                    "method": "logout",
                    "session": "HomeLabMonitor",
                },
            )
        except Exception:
            pass
        finally:
            self._sid = None
            self._client.close()

    def _entry(self, api: str, method: str, version: int = 1, **extra) -> Dict[str, Any]:
        params = {"api": api, "method": method, "version": version, "_sid": self._sid}
        params.update(extra)
        return self._get("entry.cgi", params)

    # --- high level ---------------------------------------------------------
    def system_info(self) -> Dict[str, Any]:
        return self._entry("SYNO.Core.System", "info", version=1)

    def utilization(self) -> Dict[str, Any]:
        return self._entry("SYNO.Core.System.Utilization", "get", version=1)

    def storage(self) -> Dict[str, Any]:
        return self._entry("SYNO.Storage.CGI.Storage", "load_info", version=1)


def _safe(fn, default=None):
    try:
        return fn()
    except Exception as exc:  # pragma: no cover - network dependent
        log.debug("synology sub-call failed: %s", exc)
        return default


def get_status(cfg: Dict[str, Any]) -> Dict[str, Any]:
    """Return a normalised status dict. Raises only if login fails entirely."""
    client = SynologyClient(cfg)
    out: Dict[str, Any] = {"connected": False}
    try:
        client.login()
        out["connected"] = True

        info = _safe(client.system_info, {}) or {}
        out["model"] = info.get("model")
        out["dsm_version"] = info.get("firmware_ver") or info.get("version_string")
        out["serial"] = info.get("serial")
        out["temperature_c"] = info.get("sys_temp")
        out["uptime_s"] = _to_int(info.get("up_time"))

        util = _safe(client.utilization, {}) or {}
        cpu = util.get("cpu", {})
        mem = util.get("memory", {})
        out["cpu_load"] = _cpu_load(cpu)
        out["mem_usage"] = _mem_usage(mem)

        storage = _safe(client.storage, {}) or {}
        out["volumes"] = _volumes(storage)
        if out.get("temperature_c") is None:
            out["temperature_c"] = _disk_temp(storage)
        return out
    finally:
        client.logout()


# --- normalisation helpers --------------------------------------------------
def _to_int(value) -> Optional[int]:
    try:
        # up_time can be "123456" seconds or "1 day, 02:03:04"
        if isinstance(value, str) and ":" in value:
            days = 0
            rest = value
            if "day" in value:
                day_part, rest = value.split(",", 1)
                days = int("".join(ch for ch in day_part if ch.isdigit()) or 0)
            h, m, s = (int(x) for x in rest.strip().split(":"))
            return days * 86400 + h * 3600 + m * 60 + s
        return int(value)
    except (ValueError, TypeError):
        return None


def _cpu_load(cpu: Dict[str, Any]) -> Optional[float]:
    try:
        user = float(cpu.get("user_load", 0))
        system = float(cpu.get("system_load", 0))
        return round(user + system, 1)
    except (ValueError, TypeError):
        return None


def _mem_usage(mem: Dict[str, Any]) -> Optional[float]:
    try:
        real = float(mem.get("real_usage"))
        return round(real, 1)
    except (ValueError, TypeError):
        try:
            total = float(mem.get("total_real", mem.get("memory_size", 0)))
            avail = float(mem.get("avail_real", 0))
            if total:
                return round((total - avail) / total * 100, 1)
        except (ValueError, TypeError):
            pass
        return None


def _volumes(storage: Dict[str, Any]) -> list:
    out = []
    for vol in storage.get("volumes", []) or []:
        size = vol.get("size", {})
        total = _to_int(size.get("total"))
        used = _to_int(size.get("used"))
        pct = round(used / total * 100, 1) if total and used is not None else None
        out.append(
            {
                "id": vol.get("id"),
                "status": vol.get("status"),
                "fs_type": vol.get("fs_type"),
                "total_bytes": total,
                "used_bytes": used,
                "used_pct": pct,
            }
        )
    return out


def _disk_temp(storage: Dict[str, Any]) -> Optional[float]:
    temps = [d.get("temp") for d in storage.get("disks", []) or [] if d.get("temp")]
    if temps:
        try:
            return round(max(float(t) for t in temps), 1)
        except (ValueError, TypeError):
            return None
    return None
