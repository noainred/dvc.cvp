"""FastAPI application: API + static frontend + scheduler lifecycle + auth."""

from __future__ import annotations

import logging
import secrets
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.responses import HTMLResponse, JSONResponse
from fastapi.staticfiles import StaticFiles
from sqlalchemy import func, select
from starlette.middleware.sessions import SessionMiddleware

from . import __version__
from .config import ROOT_DIR, config
from .database import SessionLocal, init_db, session_scope
from .models import Device, SynologyNas
from .scheduler import shutdown as sched_shutdown
from .scheduler import start as sched_start
from .services import auth as auth_svc
from .services import settings as settings_svc

logging.basicConfig(
    level=getattr(logging, config.log_level, logging.INFO),
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
)
log = logging.getLogger("homelab")

FRONTEND_DIR = ROOT_DIR / "frontend"

# Open endpoints reachable without a session (login flow + health).
_OPEN_API = {"/api/health", "/api/auth/state", "/api/auth/login", "/api/auth/logout"}


def _session_secret() -> str:
    """Stable per-install secret so login sessions survive restarts."""
    path = config.db_path.parent / "session.secret"
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        return path.read_text(encoding="utf-8").strip()
    value = secrets.token_hex(32)
    path.write_text(value, encoding="utf-8")
    return value


def _client_ip(request) -> str:
    xff = request.headers.get("x-forwarded-for")
    if xff:
        return xff.split(",")[0].strip()
    return request.client.host if request.client else "unknown"


def _seed_devices() -> None:
    with session_scope() as db:
        settings_svc.seed_defaults(db)
        if db.execute(select(func.count()).select_from(Device)).scalar():
            return
        for entry in config.get("devices", []) or []:
            if not entry.get("name") or not entry.get("host"):
                continue
            db.add(
                Device(
                    name=entry["name"],
                    host=entry["host"],
                    type=entry.get("type", "other"),
                    check_method=entry.get("check_method", "icmp"),
                    check_port=entry.get("check_port"),
                    note=entry.get("note"),
                )
            )
        log.info("Seeded initial devices from config")


def _seed_synology() -> None:
    """Seed NAS units, migrating any legacy single-Synology setting."""
    with session_scope() as db:
        if db.execute(select(func.count()).select_from(SynologyNas)).scalar():
            return
        seeded = False
        legacy = settings_svc.get(db, "synology")
        if isinstance(legacy, dict) and legacy.get("host") and legacy.get("username"):
            db.add(
                SynologyNas(
                    name="시놀로지",
                    host=legacy["host"],
                    port=legacy.get("port", 5001),
                    https=legacy.get("https", True),
                    verify_ssl=legacy.get("verify_ssl", False),
                    username=legacy.get("username", ""),
                    password=legacy.get("password", ""),
                    enabled=legacy.get("enabled", True),
                )
            )
            seeded = True
        for n in config.get("synology_nas", []) or []:
            if n.get("host"):
                db.add(
                    SynologyNas(
                        name=n.get("name", "NAS"),
                        host=n["host"],
                        port=n.get("port", 5001),
                        https=n.get("https", True),
                        verify_ssl=n.get("verify_ssl", False),
                        username=n.get("username", ""),
                        password=n.get("password", ""),
                        enabled=n.get("enabled", True),
                    )
                )
                seeded = True
        if seeded:
            log.info("Seeded Synology NAS")


@asynccontextmanager
async def lifespan(_app: FastAPI):
    init_db()
    _seed_devices()
    _seed_synology()
    sched_start()
    log.info("HomeLab Monitor v%s ready on %s:%s", __version__, config.host, config.port)
    try:
        yield
    finally:
        sched_shutdown()


app = FastAPI(title="HomeLab Monitor", version=__version__, lifespan=lifespan)

# Routers
from .api import auth as auth_api  # noqa: E402
from .api import devices as devices_api  # noqa: E402
from .api import openvpn as openvpn_api  # noqa: E402
from .api import router as router_api  # noqa: E402
from .api import scan as scan_api  # noqa: E402
from .api import security as security_api  # noqa: E402
from .api import settings as settings_api  # noqa: E402
from .api import speedtest as speedtest_api  # noqa: E402
from .api import synology as synology_api  # noqa: E402
from .api import system as system_api  # noqa: E402
from .api import tailscale as tailscale_api  # noqa: E402

for r in (
    devices_api, speedtest_api, synology_api, router_api, settings_api,
    system_api, scan_api, tailscale_api, auth_api, security_api, openvpn_api,
):
    app.include_router(r.router)


@app.get("/api/health")
def health():
    return {"status": "ok", "version": __version__}


# --- Auth guard + IP-ban enforcement ---------------------------------------
@app.middleware("http")
async def auth_guard(request, call_next):
    path = request.url.path
    db = SessionLocal()
    try:
        # Enforced only when login is on AND a verified account exists (no lockout).
        enabled = auth_svc.auth_required(db)
        ban = auth_svc.is_banned(db, _client_ip(request)) if enabled else None
    finally:
        db.close()

    if enabled and ban and path.startswith("/api/auth/login"):
        return JSONResponse({"detail": "로그인 시도가 너무 많아 IP가 차단되었습니다", "banned": True}, status_code=429)
    if not enabled:
        return await call_next(request)
    if path == "/" or path == "/favicon.ico" or path.startswith("/static") or path in _OPEN_API:
        return await call_next(request)
    if path.startswith("/api"):
        if request.session.get("uid"):
            return await call_next(request)
        return JSONResponse({"detail": "인증이 필요합니다"}, status_code=401)
    return await call_next(request)


# SessionMiddleware is added last so it wraps (runs before) the guard above,
# making request.session available inside it.
app.add_middleware(SessionMiddleware, secret_key=_session_secret(), max_age=14 * 24 * 3600)


# --- Static frontend --------------------------------------------------------
@app.get("/")
def index():
    # Serve the HTML uncached and cache-bust the assets by version so a browser
    # never gets stuck on a stale app.js/style.css after an update.
    html = (FRONTEND_DIR / "index.html").read_text(encoding="utf-8")
    html = html.replace("/static/app.js", f"/static/app.js?v={__version__}")
    html = html.replace("/static/style.css", f"/static/style.css?v={__version__}")
    return HTMLResponse(html, headers={"Cache-Control": "no-cache, no-store, must-revalidate"})


if FRONTEND_DIR.is_dir():
    app.mount("/static", StaticFiles(directory=str(FRONTEND_DIR)), name="static")
