"""Version reporting and self-upgrade.

Provides the running version (app version + git commit), an update check against
the configured git remote, and an in-place upgrade (git pull -> pip install ->
restart). Designed to work both under systemd and when launched manually with
``python run.py`` by re-executing the current process image.
"""

from __future__ import annotations

import datetime as dt
import json
import logging
import os
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import Any, Dict

from .. import __version__
from ..config import ROOT_DIR, config

log = logging.getLogger(__name__)

# In-memory progress for the current/last upgrade in this process lifetime.
_upgrade_state: Dict[str, Any] = {
    "status": "idle",  # idle | running | success | error
    "started": None,
    "finished": None,
    "ok": None,
    "log": "",
}
_lock = threading.Lock()


def _state_file() -> Path:
    return config.db_path.parent / "last_upgrade.json"


def _git(*args: str, timeout: int = 60) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", *args],
        cwd=str(ROOT_DIR),
        capture_output=True,
        text=True,
        timeout=timeout,
    )


def is_git_repo() -> bool:
    return (ROOT_DIR / ".git").exists()


def get_version_info() -> Dict[str, Any]:
    info: Dict[str, Any] = {"version": __version__, "git": is_git_repo()}
    if not info["git"]:
        return info
    try:
        info["commit"] = _git("rev-parse", "--short", "HEAD").stdout.strip() or None
        info["branch"] = _git("rev-parse", "--abbrev-ref", "HEAD").stdout.strip() or None
        info["commit_date"] = _git("log", "-1", "--format=%cI").stdout.strip() or None
    except Exception as exc:  # pragma: no cover
        log.debug("version info failed: %s", exc)
    return info


def _upstream_ref() -> str:
    r = _git("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
    if r.returncode == 0 and r.stdout.strip():
        return r.stdout.strip()
    branch = _git("rev-parse", "--abbrev-ref", "HEAD").stdout.strip() or "main"
    return f"origin/{branch}"


def check_updates(fetch: bool = True) -> Dict[str, Any]:
    """Compare the local checkout against its upstream branch."""
    if not is_git_repo():
        return {"supported": False, "reason": "git 저장소가 아닙니다", "current": get_version_info()}
    try:
        if fetch:
            f = _git("fetch", "--quiet", "--prune", timeout=90)
            if f.returncode != 0:
                return {
                    "supported": True,
                    "error": (f.stderr or "fetch 실패").strip()[:300],
                    "current": get_version_info(),
                }
        upstream = _upstream_ref()
        behind = int((_git("rev-list", "--count", f"HEAD..{upstream}").stdout or "0").strip() or 0)
        ahead = int((_git("rev-list", "--count", f"{upstream}..HEAD").stdout or "0").strip() or 0)
        latest_msg = _git("log", "-1", "--format=%s", upstream).stdout.strip()
        latest_date = _git("log", "-1", "--format=%cI", upstream).stdout.strip()
        return {
            "supported": True,
            "current": get_version_info(),
            "upstream": upstream,
            "behind": behind,
            "ahead": ahead,
            "up_to_date": behind == 0,
            "latest_message": latest_msg or None,
            "latest_date": latest_date or None,
        }
    except Exception as exc:  # pragma: no cover
        return {"supported": True, "error": str(exc)[:300], "current": get_version_info()}


def get_upgrade_status() -> Dict[str, Any]:
    with _lock:
        if _upgrade_state["status"] == "running":
            return dict(_upgrade_state)
    # Fall back to the persisted result of the last (possibly pre-restart) run.
    try:
        if _state_file().exists():
            return json.loads(_state_file().read_text(encoding="utf-8"))
    except Exception:
        pass
    with _lock:
        return dict(_upgrade_state)


def _persist_state() -> None:
    try:
        _state_file().parent.mkdir(parents=True, exist_ok=True)
        _state_file().write_text(json.dumps(_upgrade_state, default=str), encoding="utf-8")
    except Exception as exc:  # pragma: no cover
        log.debug("persist upgrade state failed: %s", exc)


def perform_upgrade(restart: bool = True) -> Dict[str, Any]:
    if not is_git_repo():
        return {"status": "error", "error": "git 저장소가 아니어서 업그레이드할 수 없습니다"}
    with _lock:
        if _upgrade_state["status"] == "running":
            return {"status": "running"}
        _upgrade_state.update(
            status="running", started=_now(), finished=None, ok=None, log=""
        )
    threading.Thread(target=_run_upgrade, args=(restart,), daemon=True).start()
    return {"status": "started"}


def _now() -> str:
    return dt.datetime.utcnow().isoformat() + "Z"


def _append(line: str) -> None:
    with _lock:
        _upgrade_state["log"] += line.rstrip() + "\n"
    log.info("[upgrade] %s", line.rstrip())


def _finish(status: str, ok: bool) -> None:
    with _lock:
        _upgrade_state.update(status=status, ok=ok, finished=_now())
        _persist_state()


def _run_upgrade(restart: bool) -> None:
    try:
        _append("git fetch ...")
        _git("fetch", "--all", "--prune", timeout=120)

        before = _git("rev-parse", "HEAD").stdout.strip()
        _append("git pull --ff-only ...")
        pull = _git("pull", "--ff-only", timeout=120)
        _append((pull.stdout + pull.stderr).strip())
        if pull.returncode != 0:
            _append("업데이트 실패: fast-forward 할 수 없습니다 (로컬 변경 충돌 가능)")
            _finish("error", False)
            return
        after = _git("rev-parse", "HEAD").stdout.strip()

        if before == after:
            _append("이미 최신 버전입니다. 재시작하지 않습니다.")
            _finish("success", True)
            return

        _append("의존성 설치 (pip install -r requirements.txt) ...")
        pip = subprocess.run(
            [sys.executable, "-m", "pip", "install", "-q", "-r", "requirements.txt"],
            cwd=str(ROOT_DIR),
            capture_output=True,
            text=True,
            timeout=900,
        )
        if pip.stdout:
            _append(pip.stdout.strip()[-1500:])
        if pip.returncode != 0:
            _append((pip.stderr or "pip 실패").strip()[-1500:])
            _finish("error", False)
            return

        _append(f"업그레이드 완료: {before[:7]} -> {after[:7]}")
        _finish("success", True)

        if restart:
            _append("새 버전으로 재시작합니다 ...")
            _schedule_restart()
    except Exception as exc:  # pragma: no cover
        _append("오류: " + str(exc))
        _finish("error", False)


def _schedule_restart(delay: float = 2.0) -> None:
    """Restart after a short delay so the HTTP response can flush."""

    def _do() -> None:
        time.sleep(delay)
        restart_process()

    threading.Thread(target=_do, daemon=True).start()


def restart_process() -> None:
    """Restart the running server.

    Order of preference:
    1. ``HOMELAB_RESTART_CMD`` env (e.g. ``sudo systemctl restart homelab-monitor``).
    2. Re-exec the current Python process in place (works for ``python run.py``
       and, under systemd, keeps the same PID with the new code loaded).
    3. Clean exit, relying on a process manager with ``Restart=always``.
    """
    cmd = os.getenv("HOMELAB_RESTART_CMD")
    if cmd:
        try:
            subprocess.Popen(cmd, shell=True)
            return
        except Exception as exc:  # pragma: no cover
            log.warning("HOMELAB_RESTART_CMD failed: %s", exc)
    try:
        script = os.path.abspath(sys.argv[0]) if sys.argv and sys.argv[0] else ""
        argv = [sys.executable] + ([script] if script else []) + sys.argv[1:]
        log.info("Re-executing: %s", " ".join(argv))
        os.execv(sys.executable, argv)
    except Exception as exc:  # pragma: no cover
        log.warning("re-exec failed (%s); exiting for supervisor restart", exc)
        os._exit(0)
