"""OpenVPN server status, client profiles and management.

Read-only status (installed?, running?, connected clients, .ovpn profiles) is
fully self-contained. Mutating operations (install/add/revoke) shell out to
``deploy/openvpn-setup.sh`` which requires root; they are best-effort and degrade
gracefully when sudo is not permitted.
"""

from __future__ import annotations

import datetime as dt
import glob
import logging
import os
import re
import shutil
import subprocess
import threading
from pathlib import Path
from typing import Any, Dict, List, Optional

from ..config import ROOT_DIR

log = logging.getLogger(__name__)

_NAME_RE = re.compile(r"^[A-Za-z0-9_.-]{1,40}$")

# In-memory state of the last add/revoke/install action.
_action: Dict[str, Any] = {"status": "idle", "cmd": None, "log": "", "ok": None, "ts": None}
_lock = threading.Lock()


def valid_name(name: str) -> bool:
    return bool(_NAME_RE.match(name or ""))


def is_installed() -> bool:
    return shutil.which("openvpn") is not None and Path("/etc/openvpn/server.conf").exists()


def service_active() -> Optional[str]:
    for svc in ("openvpn-server@server", "openvpn@server", "openvpn"):
        try:
            r = subprocess.run(["systemctl", "is-active", svc], capture_output=True, text=True, timeout=5)
            if r.stdout.strip() == "active":
                return svc
        except Exception:
            pass
    return None


def parse_status(path: str) -> List[Dict[str, Any]]:
    clients: List[Dict[str, Any]] = []
    try:
        text = Path(path).read_text(encoding="utf-8", errors="ignore")
    except Exception:
        return clients
    in_clients = False
    for line in text.splitlines():
        if line.startswith("Common Name"):
            in_clients = True
            continue
        if line.startswith("ROUTING TABLE") or line.startswith("GLOBAL STATS"):
            in_clients = False
        if in_clients and "," in line:
            parts = line.split(",")
            if len(parts) >= 5 and parts[0] not in ("UNDEF", ""):
                clients.append(
                    {
                        "name": parts[0],
                        "address": parts[1],
                        "bytes_recv": int(parts[2]) if parts[2].isdigit() else None,
                        "bytes_sent": int(parts[3]) if parts[3].isdigit() else None,
                        "since": parts[4].strip(),
                    }
                )
    return clients


def list_profiles(ovpn_dirs: List[str]) -> List[Dict[str, Any]]:
    out: List[Dict[str, Any]] = []
    seen = set()
    for d in ovpn_dirs:
        try:
            for p in sorted(glob.glob(os.path.join(os.path.expanduser(d), "*.ovpn"))):
                name = Path(p).stem
                if name in seen:
                    continue
                seen.add(name)
                out.append({"name": name, "path": p, "size": os.path.getsize(p)})
        except Exception:
            pass
    return out


def find_profile(name: str, ovpn_dirs: List[str]) -> Optional[str]:
    if not valid_name(name):
        return None
    for prof in list_profiles(ovpn_dirs):
        if prof["name"] == name:
            return prof["path"]
    return None


def get_status(cfg: Dict[str, Any]) -> Dict[str, Any]:
    if not is_installed():
        return {"installed": False, "script_present": (ROOT_DIR / "deploy" / "openvpn-setup.sh").exists()}
    svc = service_active()
    return {
        "installed": True,
        "running": bool(svc),
        "service": svc,
        "port": cfg.get("port", 1194),
        "clients": parse_status(cfg.get("status_log", "/var/log/openvpn/status.log")),
        "profiles": list_profiles(cfg.get("ovpn_dirs", ["~", "/root", "/etc/openvpn"])),
    }


# --- mutating actions (require root) ----------------------------------------
def action_state() -> Dict[str, Any]:
    with _lock:
        return dict(_action)


def run_action(cmd: str, name: Optional[str] = None) -> Dict[str, Any]:
    if cmd not in ("install", "add", "revoke"):
        return {"status": "error", "error": "알 수 없는 명령"}
    if cmd in ("add", "revoke") and not valid_name(name or ""):
        return {"status": "error", "error": "클라이언트 이름은 영문/숫자/._- 만 허용"}
    with _lock:
        if _action["status"] == "running":
            return {"status": "running"}
        _action.update(status="running", cmd=cmd, log="", ok=None, ts=dt.datetime.utcnow().isoformat() + "Z")
    threading.Thread(target=_worker, args=(cmd, name), daemon=True).start()
    return {"status": "started", "cmd": cmd}


def _worker(cmd: str, name: Optional[str]) -> None:
    script = str(ROOT_DIR / "deploy" / "openvpn-setup.sh")
    args = ["sudo", "-n", "bash", script, cmd]
    if name:
        args.append(name)
    try:
        proc = subprocess.run(args, capture_output=True, text=True, timeout=900)
        out = (proc.stdout or "") + (proc.stderr or "")
        ok = proc.returncode == 0
        if not ok and "sudo" in out.lower() and "password" in out.lower():
            out += "\n\n[안내] passwordless sudo 가 없어 웹에서 실행할 수 없습니다. 터미널에서 직접 실행하세요:\n  sudo bash deploy/openvpn-setup.sh " + cmd + (" " + name if name else "")
        with _lock:
            _action.update(status="success" if ok else "error", ok=ok, log=out.strip()[-4000:])
    except subprocess.TimeoutExpired:
        with _lock:
            _action.update(status="error", ok=False, log="시간 초과 (설치는 터미널에서 진행하세요)")
    except Exception as exc:  # pragma: no cover
        with _lock:
            _action.update(status="error", ok=False, log=str(exc))
