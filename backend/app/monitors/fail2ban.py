"""fail2ban (OS-level) integration for Raspberry Pi OS.

Reads jail status from ``fail2ban-client`` and can write a jail policy file.
Both reading and (especially) writing require privileges the app may not have,
so everything is best-effort and degrades gracefully. To fully control fail2ban
from the web, grant the service passwordless sudo for ``fail2ban-client`` and
write access to ``/etc/fail2ban/jail.d/`` (see README).
"""

from __future__ import annotations

import logging
import re
import shutil
import subprocess
from pathlib import Path
from typing import Any, Dict

log = logging.getLogger(__name__)

JAIL_FILE = Path("/etc/fail2ban/jail.d/homelab-monitor.local")


def is_installed() -> bool:
    return shutil.which("fail2ban-client") is not None


def _run(args, timeout=8) -> subprocess.CompletedProcess:
    return subprocess.run(args, capture_output=True, text=True, timeout=timeout)


def status(jail: str = "sshd") -> Dict[str, Any]:
    if not is_installed():
        return {"installed": False}
    out: Dict[str, Any] = {"installed": True, "jail": jail}
    try:
        r = _run(["fail2ban-client", "status", jail])
        if r.returncode != 0:
            out["error"] = (r.stderr or r.stdout or "상태 조회 실패").strip()[:300]
            return out
        text = r.stdout
        out["raw"] = text.strip()
        m = re.search(r"Currently banned:\s*(\d+)", text)
        out["currently_banned"] = int(m.group(1)) if m else None
        m = re.search(r"Total banned:\s*(\d+)", text)
        out["total_banned"] = int(m.group(1)) if m else None
        m = re.search(r"Total failed:\s*(\d+)", text)
        out["total_failed"] = int(m.group(1)) if m else None
        m = re.search(r"Banned IP list:\s*(.*)", text)
        out["banned_ips"] = m.group(1).split() if m and m.group(1).strip() else []
    except Exception as exc:  # pragma: no cover
        out["error"] = str(exc)
    return out


def render_policy(jail: str, maxretry: int, bantime_minutes: int, findtime_minutes: int) -> str:
    return (
        f"[{jail}]\n"
        f"enabled = true\n"
        f"maxretry = {maxretry}\n"
        f"bantime = {bantime_minutes * 60}\n"
        f"findtime = {findtime_minutes * 60}\n"
    )


def apply_policy(jail: str, maxretry: int, bantime_minutes: int, findtime_minutes: int) -> Dict[str, Any]:
    content = render_policy(jail, maxretry, bantime_minutes, findtime_minutes)
    if not is_installed():
        return {"ok": False, "installed": False, "content": content,
                "error": "fail2ban 이 설치되어 있지 않습니다"}
    try:
        JAIL_FILE.parent.mkdir(parents=True, exist_ok=True)
        JAIL_FILE.write_text(content, encoding="utf-8")
    except PermissionError:
        return {
            "ok": False,
            "need_sudo": True,
            "path": str(JAIL_FILE),
            "content": content,
            "error": "권한이 없어 설정 파일을 쓸 수 없습니다. README의 sudo 설정을 참고하거나 아래 내용을 수동으로 저장하세요.",
        }
    except Exception as exc:  # pragma: no cover
        return {"ok": False, "error": str(exc), "content": content}

    reload_res = _run(["fail2ban-client", "reload"])
    return {
        "ok": reload_res.returncode == 0,
        "path": str(JAIL_FILE),
        "reload_output": (reload_res.stdout + reload_res.stderr).strip()[:300],
    }


def unban(jail: str, ip: str) -> Dict[str, Any]:
    if not is_installed():
        return {"ok": False, "error": "fail2ban 미설치"}
    r = _run(["fail2ban-client", "set", jail, "unbanip", ip])
    return {"ok": r.returncode == 0, "output": (r.stdout + r.stderr).strip()[:200]}
