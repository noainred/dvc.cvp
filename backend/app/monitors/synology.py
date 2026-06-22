"""Minimal Synology DSM Web API client (multi-NAS capable, 2FA aware).

Authenticates against DSM and pulls system information, resource utilisation and
storage/volume status. Designed to fail soft: any sub-call that errors simply
omits its fields rather than failing the whole status fetch.

2-step verification (OTP):
* If the account has 2FA enabled, a plain login is rejected with code 403.
* Provide a current ``otp_code`` once; we log in with ``enable_device_token`` and
  capture the returned trusted-device id (``did``). Persist that ``device_id``
  and reuse it on later logins so OTP is no longer required (unattended polling).

DSM API reference: ``/webapi/auth.cgi`` (login) and ``/webapi/entry.cgi``.
"""

from __future__ import annotations

import logging
from typing import Any, Dict, Optional

import httpx

log = logging.getLogger(__name__)

# Synology auth error codes -> friendly Korean messages.
AUTH_ERRORS = {
    400: "계정 또는 비밀번호가 올바르지 않습니다",
    401: "계정이 비활성화되어 있습니다",
    402: "권한이 없습니다",
    403: "2단계 인증(OTP) 코드가 필요합니다. OTP 코드를 입력하거나 전용 계정의 2단계 인증을 해제하세요",
    404: "2단계 인증(OTP) 코드가 올바르지 않거나 만료되었습니다 (30초 내 재입력 필요)",
    406: "강력한 비밀번호로 변경이 필요합니다",
    407: "허용되지 않은 호스트(IP)입니다",
    408: "비밀번호가 만료되었습니다",
}


class SynologyError(Exception):
    def __init__(self, message: str, code: Optional[int] = None, otp_required: bool = False):
        super().__init__(message)
        self.code = code
        self.otp_required = otp_required


class SynologyClient:
    def __init__(self, cfg: Dict[str, Any]):
        scheme = "https" if cfg.get("https", True) else "http"
        self.base = f"{scheme}://{cfg['host']}:{cfg.get('port', 5001)}/webapi"
        self.username = cfg.get("username", "")
        self.password = cfg.get("password", "")
        self.verify = bool(cfg.get("verify_ssl", False))
        self.otp_code = (cfg.get("otp_code") or "").strip()
        self.device_id = (cfg.get("device_id") or "").strip()
        self.new_device_id: Optional[str] = None
        self._sid: Optional[str] = None
        self._client = httpx.Client(verify=self.verify, timeout=15.0)

    def _get(self, path: str, params: Dict[str, Any]) -> Dict[str, Any]:
        resp = self._client.get(f"{self.base}/{path}", params=params)
        resp.raise_for_status()
        data = resp.json()
        if not data.get("success"):
            err = data.get("error", {})
            code = err.get("code") if isinstance(err, dict) else None
            raise SynologyError(f"API error: {err}", code=code)
        return data.get("data", {})

    def login(self) -> None:
        params = {
            "api": "SYNO.API.Auth",
            "version": 7,
            "method": "login",
            "account": self.username,
            "passwd": self.password,
            "session": "HomeLabMonitor",
            "format": "sid",
        }
        if self.otp_code:
            params["otp_code"] = self.otp_code
            params["enable_device_token"] = "yes"
        if self.device_id:
            params["device_id"] = self.device_id

        try:
            data = self._get("auth.cgi", params)
        except SynologyError as exc:
            code = exc.code
            friendly = AUTH_ERRORS.get(code or -1)
            if friendly:
                raise SynologyError(friendly, code=code, otp_required=code in (403, 404)) from exc
            raise

        self._sid = data.get("sid")
        self.new_device_id = data.get("did") or None
        if not self._sid:
            raise SynologyError("로그인 응답에 sid가 없습니다")

    def logout(self) -> None:
        if self._sid:
            try:
                self._get(
                    "auth.cgi",
                    {
                        "api": "SYNO.API.Auth",
                        "version": 7,
                        "method": "logout",
                        "session": "HomeLabMonitor",
                    },
                )
            except Exception:
                pass
        self._sid = None
        self._client.close()

    def _entry(self, api: str, method: str, version: int = 1, **extra) -> Dict[str, Any]:
        params = {"api": api, "method": method, "version": version, "_sid": self._sid}
        params.update(extra)
        return self._get("entry.cgi", params)

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
    """Return a normalised status dict.

    On success includes ``device_id`` when a new trusted-device token was issued
    (the caller should persist it and clear the one-time ``otp_code``).
    Raises :class:`SynologyError` if login fails.
    """
    client = SynologyClient(cfg)
    out: Dict[str, Any] = {"connected": False}
    try:
        client.login()
        out["connected"] = True
        if client.new_device_id:
            out["device_id"] = client.new_device_id

        info = _safe(client.system_info, {}) or {}
        out["model"] = info.get("model")
        out["dsm_version"] = info.get("firmware_ver") or info.get("version_string")
        out["serial"] = info.get("serial")
        out["temperature_c"] = info.get("sys_temp")
        out["uptime_s"] = _to_int(info.get("up_time"))

        util = _safe(client.utilization, {}) or {}
        out["cpu_load"] = _cpu_load(util.get("cpu", {}))
        out["mem_usage"] = _mem_usage(util.get("memory", {}))

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
        return round(float(cpu.get("user_load", 0)) + float(cpu.get("system_load", 0)), 1)
    except (ValueError, TypeError):
        return None


def _mem_usage(mem: Dict[str, Any]) -> Optional[float]:
    try:
        return round(float(mem.get("real_usage")), 1)
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
