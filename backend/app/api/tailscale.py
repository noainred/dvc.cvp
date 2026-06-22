"""Tailscale (access-from-anywhere) endpoints."""

from __future__ import annotations

from fastapi import APIRouter

from ..config import config
from ..monitors import tailscale as ts_mod

router = APIRouter(prefix="/api/tailscale", tags=["tailscale"])


@router.get("/status")
def status():
    return ts_mod.get_status(port=config.port)


@router.post("/up")
def up():
    return ts_mod.up()
