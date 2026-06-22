"""Authentication endpoints: TOTP login, user setup, IP bans."""

from __future__ import annotations

from typing import Optional

from fastapi import APIRouter, Depends, HTTPException, Request
from pydantic import BaseModel
from sqlalchemy.orm import Session

from ..database import get_db
from ..services import auth as auth_svc

router = APIRouter(prefix="/api/auth", tags=["auth"])


def client_ip(request: Request) -> str:
    xff = request.headers.get("x-forwarded-for")
    if xff:
        return xff.split(",")[0].strip()
    return request.client.host if request.client else "unknown"


def require_admin(request: Request, db: Session = Depends(get_db)) -> None:
    """During initial setup (auth disabled) allow; otherwise require a session."""
    if not auth_svc.auth_enabled(db):
        return
    if request.session.get("uid"):
        return
    raise HTTPException(401, "인증이 필요합니다")


class LoginIn(BaseModel):
    username: str
    code: str


class UserIn(BaseModel):
    username: str


class VerifyIn(BaseModel):
    code: str


class BanIn(BaseModel):
    ip: str
    minutes: Optional[int] = None
    reason: Optional[str] = "수동 차단"


@router.get("/state")
def state(request: Request, db: Session = Depends(get_db)):
    uid = request.session.get("uid")
    return {
        "auth_enabled": auth_svc.auth_enabled(db),
        "authenticated": bool(uid),
        "username": request.session.get("username"),
        "has_user": auth_svc.has_active_user(db),
    }


@router.post("/login")
def login(payload: LoginIn, request: Request, db: Session = Depends(get_db)):
    ip = client_ip(request)
    result = auth_svc.login(db, ip, payload.username.strip(), payload.code.strip())
    if result.get("ok"):
        request.session["uid"] = result["user"]["id"]
        request.session["username"] = result["user"]["username"]
        return {"ok": True, "username": result["user"]["username"]}
    if result.get("banned"):
        raise HTTPException(429, f"로그인 시도가 너무 많아 IP가 차단되었습니다 (만료: {result.get('expires_at') or '영구'})")
    raise HTTPException(401, "사용자 또는 OTP 코드가 올바르지 않습니다")


@router.post("/logout")
def logout(request: Request):
    request.session.clear()
    return {"ok": True}


# --- user management --------------------------------------------------------
@router.get("/users", dependencies=[Depends(require_admin)])
def users(db: Session = Depends(get_db)):
    return auth_svc.list_users(db)


@router.post("/users", dependencies=[Depends(require_admin)])
def create_user(payload: UserIn, db: Session = Depends(get_db)):
    try:
        return auth_svc.create_user(db, payload.username.strip())
    except ValueError as exc:
        raise HTTPException(400, str(exc))


@router.post("/users/{user_id}/verify", dependencies=[Depends(require_admin)])
def verify_user(user_id: int, payload: VerifyIn, db: Session = Depends(get_db)):
    try:
        ok = auth_svc.verify_setup(db, user_id, payload.code.strip())
    except ValueError as exc:
        raise HTTPException(404, str(exc))
    if not ok:
        raise HTTPException(400, "OTP 코드가 올바르지 않습니다. 다시 시도하세요.")
    return {"ok": True}


@router.delete("/users/{user_id}", status_code=204, dependencies=[Depends(require_admin)])
def delete_user(user_id: int, db: Session = Depends(get_db)):
    auth_svc.delete_user(db, user_id)


# --- IP bans ----------------------------------------------------------------
@router.get("/bans", dependencies=[Depends(require_admin)])
def bans(include_inactive: bool = False, db: Session = Depends(get_db)):
    return auth_svc.list_bans(db, include_inactive=include_inactive)


@router.post("/bans", dependencies=[Depends(require_admin)])
def add_ban(payload: BanIn, db: Session = Depends(get_db)):
    ban = auth_svc.ban_ip(db, payload.ip.strip(), payload.reason or "수동 차단", payload.minutes, source="manual")
    return {"ok": True, "ip": ban.ip}


@router.delete("/bans/{ip}", status_code=204, dependencies=[Depends(require_admin)])
def remove_ban(ip: str, db: Session = Depends(get_db)):
    auth_svc.unban(db, ip)
