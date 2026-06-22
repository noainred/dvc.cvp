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


class SynologyMetric(Base):
    __tablename__ = "synology_metrics"

    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    ts: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow, index=True)
    cpu_load: Mapped[float | None] = mapped_column(Float, nullable=True)
    mem_usage: Mapped[float | None] = mapped_column(Float, nullable=True)
    temp_c: Mapped[float | None] = mapped_column(Float, nullable=True)
    uptime_s: Mapped[int | None] = mapped_column(Integer, nullable=True)
    # JSON blob of volume info / extra details
    detail: Mapped[str | None] = mapped_column(Text, nullable=True)


class Setting(Base):
    """Runtime, web-editable settings stored as JSON values."""

    __tablename__ = "settings"

    key: Mapped[str] = mapped_column(String(80), primary_key=True)
    value: Mapped[str] = mapped_column(Text, nullable=False)  # JSON-encoded
    updated_at: Mapped[dt.datetime] = mapped_column(DateTime, default=_utcnow, onupdate=_utcnow)
