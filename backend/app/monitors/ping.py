"""Device reachability checks.

Three methods are supported so that almost any home device can be monitored
without elevated privileges:

* ``icmp`` - uses the system ``ping`` binary (works as a normal user on
  Raspberry Pi OS / Linux). If the optional ``icmplib`` package is installed it
  is used instead for lower overhead.
* ``tcp``  - opens a TCP connection to ``check_port`` (great for NAS/servers).
* ``http`` - performs an HTTP(S) GET and treats any response as "up".
"""

from __future__ import annotations

import platform
import re
import socket
import subprocess
import time
from dataclasses import dataclass

try:  # optional faster ICMP
    from icmplib import ping as _icmplib_ping  # type: ignore

    _HAS_ICMPLIB = True
except Exception:  # pragma: no cover - optional dependency
    _HAS_ICMPLIB = False


@dataclass
class CheckResult:
    is_up: bool
    latency_ms: float | None = None
    detail: str | None = None


_PING_TIME_RE = re.compile(r"time[=<]\s*([\d.]+)\s*ms", re.IGNORECASE)
_IS_WINDOWS = platform.system().lower().startswith("win")


def _icmp_system(host: str, timeout: float) -> CheckResult:
    """Reachability via the OS ping binary."""
    if _IS_WINDOWS:
        cmd = ["ping", "-n", "1", "-w", str(int(timeout * 1000)), host]
    else:
        # -c 1 one packet, -W timeout (s), -n numeric output
        cmd = ["ping", "-c", "1", "-W", str(max(1, int(round(timeout)))), "-n", host]
    try:
        proc = subprocess.run(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout + 3,
            text=True,
        )
    except subprocess.TimeoutExpired:
        return CheckResult(False, None, "timeout")
    except FileNotFoundError:
        return CheckResult(False, None, "ping binary not found")

    if proc.returncode == 0:
        match = _PING_TIME_RE.search(proc.stdout or "")
        latency = float(match.group(1)) if match else None
        return CheckResult(True, latency)
    return CheckResult(False, None, "unreachable")


def _icmp(host: str, timeout: float) -> CheckResult:
    if _HAS_ICMPLIB:
        try:
            host_obj = _icmplib_ping(host, count=1, timeout=timeout, privileged=False)
            if host_obj.is_alive:
                return CheckResult(True, round(host_obj.avg_rtt, 2))
            return CheckResult(False, None, "unreachable")
        except Exception:
            # Fall back to system ping (e.g. permission issues with raw sockets)
            return _icmp_system(host, timeout)
    return _icmp_system(host, timeout)


def _tcp(host: str, port: int, timeout: float) -> CheckResult:
    start = time.perf_counter()
    try:
        with socket.create_connection((host, port), timeout=timeout):
            latency = (time.perf_counter() - start) * 1000.0
            return CheckResult(True, round(latency, 2))
    except OSError as exc:
        return CheckResult(False, None, str(exc))


def _http(host: str, port: int | None, timeout: float) -> CheckResult:
    import httpx

    url = host
    if not url.startswith(("http://", "https://")):
        scheme = "https" if port in (443, 8443) else "http"
        netloc = f"{host}:{port}" if port else host
        url = f"{scheme}://{netloc}"
    start = time.perf_counter()
    try:
        resp = httpx.get(url, timeout=timeout, verify=False, follow_redirects=True)
        latency = (time.perf_counter() - start) * 1000.0
        return CheckResult(True, round(latency, 2), f"HTTP {resp.status_code}")
    except Exception as exc:
        return CheckResult(False, None, str(exc))


def check(host: str, method: str = "icmp", port: int | None = None, timeout: float = 2.0) -> CheckResult:
    """Run a single reachability check for ``host`` using ``method``."""
    method = (method or "icmp").lower()
    if method == "tcp":
        if not port:
            return CheckResult(False, None, "tcp method requires a port")
        return _tcp(host, port, timeout)
    if method == "http":
        return _http(host, port, timeout)
    return _icmp(host, timeout)
