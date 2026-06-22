"""Pydantic request/response schemas."""

from __future__ import annotations

import datetime as dt
from typing import Any, Dict, List, Optional

from pydantic import BaseModel, Field


class DeviceBase(BaseModel):
    name: str = Field(..., min_length=1, max_length=120)
    host: str = Field(..., min_length=1, max_length=255)
    type: str = "other"
    check_method: str = "icmp"  # icmp | tcp | http
    check_port: Optional[int] = None
    enabled: bool = True
    note: Optional[str] = None


class DeviceCreate(DeviceBase):
    pass


class DeviceUpdate(BaseModel):
    name: Optional[str] = None
    host: Optional[str] = None
    type: Optional[str] = None
    check_method: Optional[str] = None
    check_port: Optional[int] = None
    enabled: Optional[bool] = None
    note: Optional[str] = None


class DeviceRead(DeviceBase):
    id: int
    last_status: Optional[bool] = None
    last_latency_ms: Optional[float] = None
    last_checked: Optional[dt.datetime] = None
    last_change: Optional[dt.datetime] = None
    created_at: Optional[dt.datetime] = None

    class Config:
        from_attributes = True


class SpeedTestRead(BaseModel):
    id: int
    ts: dt.datetime
    ok: bool
    download_mbps: Optional[float] = None
    upload_mbps: Optional[float] = None
    ping_ms: Optional[float] = None
    jitter_ms: Optional[float] = None
    server: Optional[str] = None
    isp: Optional[str] = None
    error: Optional[str] = None

    class Config:
        from_attributes = True


class SettingsUpdate(BaseModel):
    monitoring: Optional[Dict[str, Any]] = None
    retention: Optional[Dict[str, Any]] = None
    speedtest: Optional[Dict[str, Any]] = None
    scan: Optional[Dict[str, Any]] = None
    synology: Optional[Dict[str, Any]] = None
    router: Optional[Dict[str, Any]] = None
    update: Optional[Dict[str, Any]] = None
    security: Optional[Dict[str, Any]] = None
    fail2ban: Optional[Dict[str, Any]] = None
    tailscale: Optional[Dict[str, Any]] = None
