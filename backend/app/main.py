"""FastAPI application: API + static frontend + scheduler lifecycle."""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles
from sqlalchemy import func, select

from . import __version__
from .config import ROOT_DIR, config
from .database import init_db, session_scope
from .models import Device
from .scheduler import shutdown as sched_shutdown
from .scheduler import start as sched_start
from .services import settings as settings_svc

logging.basicConfig(
    level=getattr(logging, config.log_level, logging.INFO),
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
)
log = logging.getLogger("homelab")

FRONTEND_DIR = ROOT_DIR / "frontend"


def _seed_devices() -> None:
    """Register devices from config.yaml on first run (empty DB only)."""
    with session_scope() as db:
        settings_svc.seed_defaults(db)
        count = db.execute(select(func.count()).select_from(Device)).scalar()
        if count:
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


@asynccontextmanager
async def lifespan(_app: FastAPI):
    init_db()
    _seed_devices()
    sched_start()
    log.info("HomeLab Monitor v%s ready on %s:%s", __version__, config.host, config.port)
    try:
        yield
    finally:
        sched_shutdown()


app = FastAPI(title="HomeLab Monitor", version=__version__, lifespan=lifespan)

# Routers
from .api import devices as devices_api  # noqa: E402
from .api import router as router_api  # noqa: E402
from .api import settings as settings_api  # noqa: E402
from .api import speedtest as speedtest_api  # noqa: E402
from .api import synology as synology_api  # noqa: E402
from .api import system as system_api  # noqa: E402

app.include_router(devices_api.router)
app.include_router(speedtest_api.router)
app.include_router(synology_api.router)
app.include_router(router_api.router)
app.include_router(settings_api.router)
app.include_router(system_api.router)


@app.get("/api/health")
def health():
    return {"status": "ok", "version": __version__}


# --- Static frontend --------------------------------------------------------
@app.get("/")
def index():
    return FileResponse(FRONTEND_DIR / "index.html")


if FRONTEND_DIR.is_dir():
    app.mount("/static", StaticFiles(directory=str(FRONTEND_DIR)), name="static")
