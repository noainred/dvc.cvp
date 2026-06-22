"""SQLAlchemy ORM models.

Data retention strategy (queryable for 3+ years while keeping the DB small):

* ``Check``        - raw per-interval results, pruned after ``retention.raw_days``.
* ``UptimeHourly`` - hourly rollups, pruned after ``retention.hourly_days``.
* ``UptimeDaily``  - daily rollups, **kept forever** (long-term history).
* ``Event``        - UP/DOWN transitions, **kept forever** (outage timeline).
* ``SpeedTest``    - internet speed results, **kept forever** (low volume).
* ``SynologyMetric`` - NAS metrics, pruned after ``retention.hourly_days``.
"""

from __future__ import annotations

import datetime as dt

from sqlalchemy import (
    Boolean,
    Date,
    DateTime,
    Float,
    ForeignKey,
    Integer,
    String,
    Text,
    UniqueConstraint,
    Index,
)
from sqlalchemy.orm import Mapped, mapped_column, relationship

from .database import Base


def _utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


class Device(Base):
    __tablename__ = "devices"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    name: Mapped[str] = mapped_column(String(120), nullable=False)
    host: Mapped[str] = mapped_column(String(255), nullable=False)
    # router | nas | server | pc | iot | ap | printer | other
    type: Mapped[str] = mapped_column(String(40), default="other")
    # icmp | tcp | http
    check_method: Mapped[str] = mapped_column(String(10), default="icmp")
    check_port: Mapped[int | None] = mapped_column(Integer, nullable=True)
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    note: Mapped[str | None] = mapped_column(String(500), nullable=True)

    # Live state (updated each check)
    last_status: Mapped[bool | None] = mapped_column(Boolean, nullable=True)
    last_latency_ms: Mapped[float | None] = mapped_column(Float, nullable=True)
    last_checked: Mapped[dt.datetime | None] = mapped_column(DateTime, nullable=True)
    last_change: Mapped[dt.datetime | None] = mapped_column(DateTime, nullable=True)

    created_at: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)

    checks: Mapped[list["Check"]] = relationship(
        back_populates="device", cascade="all, delete-orphan"
    )


class Check(Base):
    __tablename__ = "checks"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    device_id: Mapped[int] = mapped_column(
        ForeignKey("devices.id", ondelete="CASCADE"), index=True
    )
    ts: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)
    is_up: Mapped[bool] = mapped_column(Boolean, nullable=False)
    latency_ms: Mapped[float | None] = mapped_column(Float, nullable=True)

    device: Mapped[Device] = relationship(back_populates="checks")

    __table_args__ = (Index("ix_checks_device_ts", "device_id", "ts"),)


class Event(Base):
    """UP/DOWN state transition - kept forever for the outage timeline."""

    __tablename__ = "events"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    device_id: Mapped[int] = mapped_column(
        ForeignKey("devices.id", ondelete="CASCADE"), index=True
    )
    ts: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)
    is_up: Mapped[bool] = mapped_column(Boolean, nullable=False)
    # Duration (seconds) of the *previous* state that just ended.
    prev_duration_s: Mapped[int | None] = mapped_column(Integer, nullable=True)

    __table_args__ = (Index("ix_events_device_ts", "device_id", "ts"),)


class UptimeHourly(Base):
    __tablename__ = "uptime_hourly"

    device_id: Mapped[int] = mapped_column(
        ForeignKey("devices.id", ondelete="CASCADE"), primary_key=True
    )
    hour: Mapped[dt.datetime] = mapped_column(DateTime, primary_key=True)
    total: Mapped[int] = mapped_column(Integer, default=0)
    up: Mapped[int] = mapped_column(Integer, default=0)
    latency_avg: Mapped[float | None] = mapped_column(Float, nullable=True)
    latency_min: Mapped[float | None] = mapped_column(Float, nullable=True)
    latency_max: Mapped[float | None] = mapped_column(Float, nullable=True)

    __table_args__ = (Index("ix_hourly_hour", "hour"),)


class UptimeDaily(Base):
    __tablename__ = "uptime_daily"

    device_id: Mapped[int] = mapped_column(
        ForeignKey("devices.id", ondelete="CASCADE"), primary_key=True
    )
    day: Mapped[dt.date] = mapped_column(Date, primary_key=True)
    total: Mapped[int] = mapped_column(Integer, default=0)
    up: Mapped[int] = mapped_column(Integer, default=0)
    latency_avg: Mapped[float | None] = mapped_column(Float, nullable=True)
    latency_min: Mapped[float | None] = mapped_column(Float, nullable=True)
    latency_max: Mapped[float | None] = mapped_column(Float, nullable=True)

    __table_args__ = (Index("ix_daily_day", "day"),)


class SpeedTest(Base):
    __tablename__ = "speedtests"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    ts: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow, index=True)
    ok: Mapped[bool] = mapped_column(Boolean, default=False)
    download_mbps: Mapped[float | None] = mapped_column(Float, nullable=True)
    upload_mbps: Mapped[float | None] = mapped_column(Float, nullable=True)
    ping_ms: Mapped[float | None] = mapped_column(Float, nullable=True)
    jitter_ms: Mapped[float | None] = mapped_column(Float, nullable=True)
    server: Mapped[str | None] = mapped_column(String(200), nullable=True)
    isp: Mapped[str | None] = mapped_column(String(200), nullable=True)
    error: Mapped[str | None] = mapped_column(String(500), nullable=True)


class SynologyNas(Base):
    """A Synology NAS unit. Multiple units are supported."""

    __tablename__ = "synology_nas"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    name: Mapped[str] = mapped_column(String(120), nullable=False)
    host: Mapped[str] = mapped_column(String(255), nullable=False)
    port: Mapped[int] = mapped_column(Integer, default=5001)
    https: Mapped[bool] = mapped_column(Boolean, default=True)
    verify_ssl: Mapped[bool] = mapped_column(Boolean, default=False)
    username: Mapped[str] = mapped_column(String(120), default="")
    password: Mapped[str] = mapped_column(String(255), default="")
    # 2-step verification: a transient OTP code (consumed on next login) and the
    # resulting trusted-device token reused for subsequent unattended logins.
    otp_code: Mapped[str | None] = mapped_column(String(16), nullable=True)
    device_id: Mapped[str | None] = mapped_column(String(255), nullable=True)
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    created_at: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)

    # Live state (updated each poll)
    last_connected: Mapped[bool | None] = mapped_column(Boolean, nullable=True)
    last_polled: Mapped[dt.datetime | None] = mapped_column(DateTime, nullable=True)
    last_detail: Mapped[str | None] = mapped_column(Text, nullable=True)  # JSON


class SynologyMetric(Base):
    __tablename__ = "synology_metrics"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    nas_id: Mapped[int | None] = mapped_column(
        ForeignKey("synology_nas.id", ondelete="CASCADE"), index=True, nullable=True
    )
    ts: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow, index=True)
    cpu_load: Mapped[float | None] = mapped_column(Float, nullable=True)
    mem_usage: Mapped[float | None] = mapped_column(Float, nullable=True)
    temp_c: Mapped[float | None] = mapped_column(Float, nullable=True)
    uptime_s: Mapped[int | None] = mapped_column(Integer, nullable=True)
    # JSON blob of volume info / extra details
    detail: Mapped[str | None] = mapped_column(Text, nullable=True)


class DiscoveredHost(Base):
    """A host found by the IP scanner. One row per IP (inventory)."""

    __tablename__ = "discovered_hosts"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    ip: Mapped[str] = mapped_column(String(45), unique=True, index=True)
    mac: Mapped[str | None] = mapped_column(String(32), nullable=True)
    hostname: Mapped[str | None] = mapped_column(String(255), nullable=True)
    vendor: Mapped[str | None] = mapped_column(String(120), nullable=True)
    first_seen: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)
    last_seen: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)
    last_up: Mapped[bool] = mapped_column(Boolean, default=True)
    times_seen: Mapped[int] = mapped_column(Integer, default=1)


class HostEvent(Base):
    """UP/DOWN transition for a discovered host (from IP scans).

    Recorded only on state change, so it stays small while letting us
    reconstruct how long an IP has been online over time.
    """

    __tablename__ = "host_events"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    ip: Mapped[str] = mapped_column(String(45), index=True)
    ts: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)
    is_up: Mapped[bool] = mapped_column(Boolean, nullable=False)

    __table_args__ = (Index("ix_host_events_ip_ts", "ip", "ts"),)


class ScanRun(Base):
    """One IP scan execution - kept as a lightweight history of scans."""

    __tablename__ = "scan_runs"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    ts: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow, index=True)
    subnet: Mapped[str | None] = mapped_column(String(64), nullable=True)
    total: Mapped[int] = mapped_column(Integer, default=0)
    up: Mapped[int] = mapped_column(Integer, default=0)
    new: Mapped[int] = mapped_column(Integer, default=0)
    duration_ms: Mapped[int | None] = mapped_column(Integer, nullable=True)


class User(Base):
    """A portal user that logs in with a TOTP (Google Authenticator) code."""

    __tablename__ = "users"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    username: Mapped[str] = mapped_column(String(80), unique=True, index=True)
    totp_secret: Mapped[str] = mapped_column(String(64), nullable=False)
    # Becomes True only after the user proves they scanned the QR (first code).
    enabled: Mapped[bool] = mapped_column(Boolean, default=False)
    created_at: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)
    last_login: Mapped[dt.datetime | None] = mapped_column(DateTime, nullable=True)


class LoginAttempt(Base):
    __tablename__ = "login_attempts"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    ip: Mapped[str] = mapped_column(String(45), index=True)
    username: Mapped[str | None] = mapped_column(String(80), nullable=True)
    ts: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow, index=True)
    success: Mapped[bool] = mapped_column(Boolean, default=False)


class IpBan(Base):
    __tablename__ = "ip_bans"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    ip: Mapped[str] = mapped_column(String(45), unique=True, index=True)
    reason: Mapped[str | None] = mapped_column(String(200), nullable=True)
    source: Mapped[str] = mapped_column(String(20), default="portal")  # portal | manual
    created_at: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow)
    expires_at: Mapped[dt.datetime | None] = mapped_column(DateTime, nullable=True)  # None = 영구
    active: Mapped[bool] = mapped_column(Boolean, default=True)


class Setting(Base):
    """Runtime, web-editable settings stored as JSON values."""

    __tablename__ = "settings"

    key: Mapped[str] = mapped_column(String(80), primary_key=True)
    value: Mapped[str] = mapped_column(Text, nullable=False)  # JSON-encoded
    updated_at: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow, onupdate=_utcnow)
