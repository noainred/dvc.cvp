"""Tailscale integration for access-from-anywhere.

Wraps the ``tailscale`` CLI to report connection state, the device's Tailscale
IP / MagicDNS name and ready-to-use URLs for reaching this monitor from any
device on the tailnet. Also offers a best-effort ``up`` helper that surfaces the
login URL when authentication is required.

Everything degrades gracefully when Tailscale is not installed.
"""

from __future__ import annotations

import json
import logging
import re
import shutil
import subprocess
from typing import Any, Dict, List, Optional

log = logging.getLogger(__name__)

_AUTH_URL_RE = re.compile(r"https://login\.tailscale\.com/\S+")


def is_installed() -> bool:
    return shutil.which("tailscale") is not None


def _status_json() -> Optional[Dict[str, Any]]:
    try:
        r = subprocess.run(
            ["tailscale", "status", "--json"], capture_output=True, text=True, timeout=10
        )
        if r.stdout.strip():
            return json.loads(r.stdout)
        return {"error": (r.stderr or "tailscale status 실패").strip()}
    except Exception as exc:  # pragma: no cover
        return {"error": str(exc)}


def _ipv4(ips: List[str]) -> Optional[str]:
    return next((i for i in (ips or []) if ":" not in i), None)


def get_status(port: int = 8080) -> Dict[str, Any]:
    if not is_installed():
        return {"installed": False}

    data = _status_json()
    if not data:
        return {"installed": True, "error": "상태를 읽을 수 없습니다"}
    if "BackendState" not in data and data.get("error"):
        return {"installed": True, "error": data["error"]}

    self_ = data.get("Self") or {}
    ips = self_.get("TailscaleIPs") or data.get("TailscaleIPs") or []
    ip4 = _ipv4(ips)
    dns = (self_.get("DNSName") or "").rstrip(".")
    state = data.get("BackendState")

    access_urls: List[str] = []
    if ip4:
        access_urls.append(f"http://{ip4}:{port}")
    if dns:
        access_urls.append(f"http://{dns}:{port}")

    peers: List[Dict[str, Any]] = []
    for p in (data.get("Peer") or {}).values():
        peers.append(
            {
                "hostname": p.get("HostName"),
                "ip": _ipv4(p.get("TailscaleIPs") or []),
                "online": p.get("Online"),
                "os": p.get("OS"),
            }
        )
    peers.sort(key=lambda x: (not x["online"], x["hostname"] or ""))

    return {
        "installed": True,
        "state": state,
        "running": state == "Running",
        "auth_url": data.get("AuthURL"),
        "hostname": self_.get("HostName"),
        "dns_name": dns or None,
        "ip": ip4,
        "ips": ips,
        "online": self_.get("Online"),
        "tailnet": (data.get("CurrentTailnet") or {}).get("Name"),
        "access_urls": access_urls,
        "peers": peers,
    }


def up(timeout: int = 8) -> Dict[str, Any]:
    """Best-effort ``tailscale up``; returns the login URL if one is shown."""
    if not is_installed():
        return {"ok": False, "error": "tailscale 가 설치되어 있지 않습니다"}
    try:
        r = subprocess.run(
            ["tailscale", "up"], capture_output=True, text=True, timeout=timeout
        )
        out = (r.stdout or "") + (r.stderr or "")
        m = _AUTH_URL_RE.search(out)
        return {"ok": r.returncode == 0, "auth_url": m.group(0) if m else None, "output": out.strip()[:600]}
    except subprocess.TimeoutExpired as exc:
        out = ((exc.stdout or "") + (exc.stderr or "")) if isinstance(exc.stdout, str) else ""
        m = _AUTH_URL_RE.search(out)
        # Still waiting for authentication; the URL (if any) lets the user finish.
        return {"ok": False, "pending": True, "auth_url": m.group(0) if m else None, "output": out.strip()[:600]}
    except Exception as exc:  # pragma: no cover
        return {"ok": False, "error": str(exc)}
