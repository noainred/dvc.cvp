#!/usr/bin/env python3
"""HomeLab Monitor 관리/복구 CLI.

웹 로그인에 잠겼을 때(계정 분실 등) 라즈베리파이 터미널에서 복구하는 도구입니다.
설치 폴더에서 가상환경을 켠 뒤 사용하세요:

    source .venv/bin/activate
    python manage.py disable-auth          # 로그인 요구 끄기 (즉시 잠금 해제)
    python manage.py add-user <이름>        # 계정 생성 (QR/키 출력)
    python manage.py verify-user <이름> <코드>
    python manage.py add-user <이름> --enable   # OTP 검증 없이 바로 활성화
    python manage.py enable-auth           # 로그인 요구 켜기 (활성 계정 필요)
    python manage.py list-users
    python manage.py delete-user <이름>
    python manage.py bans                   # 차단된 IP 목록
    python manage.py unban <ip>
"""

from __future__ import annotations

import argparse
import sys

import pyotp
from sqlalchemy import select

from backend.app.database import init_db, session_scope
from backend.app.models import User
from backend.app.services import auth as auth_svc
from backend.app.services import settings as settings_svc


def _seed():
    init_db()
    with session_scope() as db:
        settings_svc.seed_defaults(db)


def cmd_disable_auth(_args):
    with session_scope() as db:
        settings_svc.update(db, "security", {"auth_enabled": False})
    print("✅ 로그인 요구를 껐습니다. 웹 [보안]에서 계정을 만든 뒤 다시 켜세요.")


def cmd_enable_auth(_args):
    with session_scope() as db:
        if not auth_svc.has_active_user(db):
            print("⚠ 인증된(활성) 계정이 없어 켤 수 없습니다. 먼저 add-user 로 계정을 만드세요.")
            return
        settings_svc.update(db, "security", {"auth_enabled": True})
    print("✅ 로그인 요구를 켰습니다.")


def cmd_add_user(args):
    with session_scope() as db:
        info = auth_svc.create_user(db, args.username)
        if args.enable:
            user = db.get(User, info["id"])
            user.enabled = True
    print(f"사용자 : {info['username']}")
    print(f"비밀키 : {info['secret']}")
    print(f"otpauth: {info['otpauth_uri']}")
    try:
        import qrcode

        qr = qrcode.QRCode(border=1)
        qr.add_data(info["otpauth_uri"])
        qr.make()
        print("\n[ Google Authenticator 로 아래 QR 스캔 ]")
        qr.print_ascii(invert=True)
    except Exception:
        print("(QR 출력 실패 — 위 비밀키를 수동 입력하세요)")
    if args.enable:
        print("\n✅ 계정을 바로 활성화했습니다.")
    else:
        print(f"\n등록 후 다음으로 인증하세요:  python manage.py verify-user {args.username} <6자리코드>")


def cmd_verify_user(args):
    with session_scope() as db:
        user = db.execute(select(User).where(User.username == args.username)).scalar_one_or_none()
        if not user:
            print("⚠ 사용자를 찾을 수 없습니다.")
            return
        ok = auth_svc.verify_setup(db, user.id, args.code)
    print("✅ 인증 완료. 이제 로그인할 수 있습니다." if ok else "⚠ 코드가 올바르지 않습니다.")


def cmd_list_users(_args):
    with session_scope() as db:
        users = auth_svc.list_users(db)
    if not users:
        print("(계정 없음)")
        return
    for u in users:
        print(f"- {u['username']:<16} {'활성' if u['enabled'] else '미인증'}  최근로그인:{u['last_login'] or '-'}")


def cmd_delete_user(args):
    with session_scope() as db:
        user = db.execute(select(User).where(User.username == args.username)).scalar_one_or_none()
        if not user:
            print("⚠ 사용자를 찾을 수 없습니다.")
            return
        auth_svc.delete_user(db, user.id)
    print(f"✅ {args.username} 삭제됨")


def cmd_bans(_args):
    with session_scope() as db:
        bans = auth_svc.list_bans(db, include_inactive=False)
    if not bans:
        print("(차단된 IP 없음)")
        return
    for b in bans:
        print(f"- {b['ip']:<16} {b['reason']}  만료:{b['expires_at'] or '영구'}")


def cmd_unban(args):
    with session_scope() as db:
        auth_svc.unban(db, args.ip)
    print(f"✅ {args.ip} 차단 해제됨")


def main():
    parser = argparse.ArgumentParser(description="HomeLab Monitor 관리/복구 CLI")
    sub = parser.add_subparsers(dest="cmd", required=True)

    sub.add_parser("disable-auth", help="로그인 요구 끄기")
    sub.add_parser("enable-auth", help="로그인 요구 켜기 (활성 계정 필요)")

    p_add = sub.add_parser("add-user", help="계정 생성")
    p_add.add_argument("username")
    p_add.add_argument("--enable", action="store_true", help="OTP 검증 없이 바로 활성화")

    p_ver = sub.add_parser("verify-user", help="OTP 코드로 계정 인증")
    p_ver.add_argument("username")
    p_ver.add_argument("code")

    sub.add_parser("list-users", help="계정 목록")
    p_del = sub.add_parser("delete-user", help="계정 삭제")
    p_del.add_argument("username")

    sub.add_parser("bans", help="차단된 IP 목록")
    p_unban = sub.add_parser("unban", help="IP 차단 해제")
    p_unban.add_argument("ip")

    args = parser.parse_args()
    _seed()
    handlers = {
        "disable-auth": cmd_disable_auth,
        "enable-auth": cmd_enable_auth,
        "add-user": cmd_add_user,
        "verify-user": cmd_verify_user,
        "list-users": cmd_list_users,
        "delete-user": cmd_delete_user,
        "bans": cmd_bans,
        "unban": cmd_unban,
    }
    handlers[args.cmd](args)


if __name__ == "__main__":
    sys.exit(main())
