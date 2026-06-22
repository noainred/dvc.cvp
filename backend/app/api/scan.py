"""IP scan: discovered host inventory and on-demand scans."""

from __future__ import annotations

from fastapi import APIRouter, BackgroundTasks, Depends, HTTPException
from sqlalchemy.orm import Session

from ..database import get_db, session_scope
from ..services import scan as scan_svc
from ..services import settings as settings_svc

router = APIRouter(prefix="/api/scan", tags=["scan"])


@router.get("/hosts")
def hosts(db: Session = Depends(get_db)):
    return {
        "subnet": scan_svc.get_subnet(db),
        "last_run": scan_svc.last_run(db),
        "hosts": scan_svc.list_hosts(db),
    }


@router.get("/hosts/{host_id}/uptime")
def host_uptime(host_id: int, db: Session = Depends(get_db)):
    data = scan_svc.host_uptime(db, host_id)
    if data is None:
        raise HTTPException(404, "호스트를 찾을 수 없습니다")
    return data


def _run_scan_bg(subnet=None):
    with session_scope() as db:
        try:
            scan_svc.run_scan(db, subnet=subnet)
        except Exception:
            pass


@router.post("/run")
def run(background: BackgroundTasks, db: Session = Depends(get_db)):
    cfg = settings_svc.get(db, "scan")
    background.add_task(_run_scan_bg, cfg.get("subnet") or None)
    return {"status": "started", "subnet": scan_svc.get_subnet(db)}


@router.get("/status")
def status(db: Session = Depends(get_db)):
    return {"subnet": scan_svc.get_subnet(db), "last_run": scan_svc.last_run(db)}
