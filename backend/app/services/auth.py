"""Authentication (TOTP / Google Authenticator) and brute-force IP banning.

Login is by username + a 6-digit TOTP code only (no password). After a
configurable number of failed attempts from one IP within a time window, that IP
is banned for a configurable duration. All policy is web-controllable via the
``security`` settings group.
"""

from __future__ import annotations

import datetime as dt
import io
import logging
from typing import Any, Dict, List, Optional

import pyotp
import qrcode
import qrcode.image.svg
from sqlalchemy import func, select
from sqlalchemy.orm import Session

from ..models import IpBan, LoginAttempt, User
from . import settings as settings_svc

log = logging.getLogger(__name__)

ISSUER = "HomeLab Monitor"


def _now() -> dt.datetime:
    return dt.datetime.utcnow()


# --- policy -----------------------------------------------------------------
def security_cfg(db: Session) -> Dict[str, Any]:
    return settings_svc.get(db, "security")


def auth_enabled(db: Session) -> bool:
    return bool(security_cfg(db).get("auth_enabled"))


def has_active_user(db: Session) -> bool:
    return (
        db.execute(select(func.count()).select_from(User).where(User.enabled.is_(True))).scalar() or 0
    ) > 0


# --- users ------------------------------------------------------------------
def list_users(db: Session) -> List[Dict[str, Any]]:
    users = db.execute(select(User).order_by(User.username)).scalars().all()
    return [
        {
            "id": u.id,
            "username": u.username,
            "enabled": u.enabled,
            "created_at": u.created_at.isoformat() + "Z" if u.created_at else None,
            "last_login": u.last_login.isoformat() + "Z" if u.last_login else None,
        }
        for u in users
    ]


def create_user(db: Session, username: str) -> Dict[str, Any]:
    existing = db.execute(select(User).where(User.username == username)).scalar_one_or_none()
    if existing is not None:
        # Re-issue a fresh secret for an unverified/!enabled account.
        if existing.enabled:
            raise ValueError("이미 존재하는 사용자입니다")
        existing.totp_secret = pyotp.random_base32()
        user = existing
    else:
        user = User(username=username, totp_secret=pyotp.random_base32(), enabled=False)
        db.add(user)
    db.commit()
    db.refresh(user)
    uri = pyotp.TOTP(user.totp_secret).provisioning_uri(name=username, issuer_name=ISSUER)
    return {
        "id": user.id,
        "username": user.username,
        "secret": user.totp_secret,
        "otpauth_uri": uri,
        "qr_svg": qr_svg(uri),
    }


def qr_svg(uri: str) -> str:
    img = qrcode.make(uri, image_factory=qrcode.image.svg.SvgPathImage, box_size=8, border=2)
    buf = io.BytesIO()
    img.save(buf)
    return buf.getvalue().decode("utf-8")


def verify_setup(db: Session, user_id: int, code: str) -> bool:
    user = db.get(User, user_id)
    if not user:
        raise ValueError("사용자를 찾을 수 없습니다")
    if pyotp.TOTP(user.totp_secret).verify(code, valid_window=1):
        user.enabled = True
        db.commit()
        return True
    return False


def delete_user(db: Session, user_id: int) -> None:
    user = db.get(User, user_id)
    if user:
        db.delete(user)
        db.commit()


# --- IP bans ----------------------------------------------------------------
def is_banned(db: Session, ip: str) -> Optional[IpBan]:
    ban = db.execute(
        select(IpBan).where(IpBan.ip == ip, IpBan.active.is_(True))
    ).scalar_one_or_none()
    if not ban:
        return None
    if ban.expires_at and ban.expires_at < _now():
        ban.active = False
        db.commit()
        return None
    return ban


def ban_ip(db: Session, ip: str, reason: str, minutes: Optional[int], source: str = "manual") -> IpBan:
    expires = _now() + dt.timedelta(minutes=minutes) if minutes else None
    ban = db.execute(select(IpBan).where(IpBan.ip == ip)).scalar_one_or_none()
    if ban is None:
        ban = IpBan(ip=ip, reason=reason, source=source, expires_at=expires, active=True)
        db.add(ban)
    else:
        ban.reason = reason
        ban.source = source
        ban.expires_at = expires
        ban.active = True
        ban.created_at = _now()
    db.commit()
    log.warning("IP %s 차단: %s (만료: %s)", ip, reason, expires)
    return ban


def unban(db: Session, ip: str) -> None:
    ban = db.execute(select(IpBan).where(IpBan.ip == ip)).scalar_one_or_none()
    if ban:
        ban.active = False
        db.commit()


def list_bans(db: Session, include_inactive: bool = False) -> List[Dict[str, Any]]:
    stmt = select(IpBan).order_by(IpBan.created_at.desc())
    if not include_inactive:
        stmt = stmt.where(IpBan.active.is_(True))
    rows = db.execute(stmt).scalars().all()
    out = []
    for b in rows:
        out.append(
            {
                "ip": b.ip,
                "reason": b.reason,
                "source": b.source,
                "created_at": b.created_at.isoformat() + "Z" if b.created_at else None,
                "expires_at": b.expires_at.isoformat() + "Z" if b.expires_at else None,
                "active": b.active,
            }
        )
    return out


# --- login ------------------------------------------------------------------
def _record_attempt(db: Session, ip: str, username: Optional[str], success: bool) -> Optional[IpBan]:
    db.add(LoginAttempt(ip=ip, username=username, success=success, ts=_now()))
    db.commit()
    if success:
        return None
    cfg = security_cfg(db)
    window = int(cfg.get("window_minutes", 10))
    max_attempts = int(cfg.get("max_attempts", 3))
    ban_minutes = int(cfg.get("ban_minutes", 15))
    cutoff = _now() - dt.timedelta(minutes=window)
    fails = (
        db.execute(
            select(func.count())
            .select_from(LoginAttempt)
            .where(LoginAttempt.ip == ip, LoginAttempt.success.is_(False), LoginAttempt.ts >= cutoff)
        ).scalar()
        or 0
    )
    if fails >= max_attempts:
        return ban_ip(
            db, ip, reason=f"{fails}회 연속 로그인 실패", minutes=ban_minutes or None, source="portal"
        )
    return None


def login(db: Session, ip: str, username: str, code: str) -> Dict[str, Any]:
    ban = is_banned(db, ip)
    if ban:
        return {"ok": False, "banned": True, "expires_at": ban.expires_at.isoformat() + "Z" if ban.expires_at else None}

    user = db.execute(
        select(User).where(User.username == username, User.enabled.is_(True))
    ).scalar_one_or_none()
    ok = bool(user) and pyotp.TOTP(user.totp_secret).verify(code, valid_window=1)

    new_ban = _record_attempt(db, ip, username, ok)
    if ok:
        user.last_login = _now()
        db.commit()
        return {"ok": True, "user": {"id": user.id, "username": user.username}}
    if new_ban:
        return {"ok": False, "banned": True, "expires_at": new_ban.expires_at.isoformat() + "Z" if new_ban.expires_at else None}
    return {"ok": False}
