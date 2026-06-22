"""Internet speed test runner.

Supports two backends:

* ``ookla``  - the official ``speedtest`` CLI (`speedtest --format=json`).
* ``python`` - the pure-python ``speedtest-cli`` library.

With ``method: auto`` (default) the Ookla binary is used when available,
otherwise the python library is tried.
"""

from __future__ import annotations

import json
import shutil
import subprocess
from dataclasses import asdict, dataclass


@dataclass
class SpeedResult:
    ok: bool
    download_mbps: float | None = None
    upload_mbps: float | None = None
    ping_ms: float | None = None
    jitter_ms: float | None = None
    server: str | None = None
    isp: str | None = None
    error: str | None = None

    def as_dict(self) -> dict:
        return asdict(self)


def _ookla_available() -> bool:
    return shutil.which("speedtest") is not None


def _run_ookla(timeout: int = 120) -> SpeedResult:
    try:
        proc = subprocess.run(
            [
                "speedtest",
                "--format=json",
                "--accept-license",
                "--accept-gdpr",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
            text=True,
        )
    except subprocess.TimeoutExpired:
        return SpeedResult(False, error="ookla speedtest timeout")
    except FileNotFoundError:
        return SpeedResult(False, error="speedtest binary not found")

    if proc.returncode != 0:
        return SpeedResult(False, error=(proc.stderr or "speedtest failed").strip()[:500])

    try:
        data = json.loads(proc.stdout)
        # bandwidth is in bytes/s -> Mbps
        dl = data["download"]["bandwidth"] * 8 / 1_000_000
        ul = data["upload"]["bandwidth"] * 8 / 1_000_000
        ping = data["ping"]["latency"]
        jitter = data["ping"].get("jitter")
        server = data.get("server", {})
        server_name = f"{server.get('name', '')} ({server.get('location', '')})".strip()
        return SpeedResult(
            ok=True,
            download_mbps=round(dl, 2),
            upload_mbps=round(ul, 2),
            ping_ms=round(ping, 2),
            jitter_ms=round(jitter, 2) if jitter is not None else None,
            server=server_name or None,
            isp=data.get("isp"),
        )
    except (KeyError, ValueError, TypeError) as exc:
        return SpeedResult(False, error=f"parse error: {exc}")


def _run_python() -> SpeedResult:
    try:
        import speedtest  # type: ignore
    except Exception:
        return SpeedResult(False, error="speedtest-cli library not installed")

    try:
        st = speedtest.Speedtest(secure=True)
        st.get_best_server()
        download = st.download() / 1_000_000  # bits/s -> Mbps
        upload = st.upload(pre_allocate=False) / 1_000_000
        results = st.results.dict()
        server = results.get("server", {})
        server_name = f"{server.get('sponsor', '')} - {server.get('name', '')}".strip(" -")
        return SpeedResult(
            ok=True,
            download_mbps=round(download, 2),
            upload_mbps=round(upload, 2),
            ping_ms=round(results.get("ping", 0), 2),
            server=server_name or None,
            isp=results.get("client", {}).get("isp"),
        )
    except Exception as exc:  # pragma: no cover - network dependent
        return SpeedResult(False, error=str(exc)[:500])


def run_speedtest(method: str = "auto") -> SpeedResult:
    method = (method or "auto").lower()
    if method == "ookla":
        return _run_ookla()
    if method == "python":
        return _run_python()
    # auto
    if _ookla_available():
        result = _run_ookla()
        if result.ok:
            return result
    return _run_python()
