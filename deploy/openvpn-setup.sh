#!/usr/bin/env bash
# ============================================================================
#  OpenVPN 서버 설치 / 클라이언트 관리 래퍼
#  검증된 설치기 angristan/openvpn-install 를 사용합니다.
#
#  사용 (root 필요):
#    sudo bash deploy/openvpn-setup.sh install        # 서버 설치(기본값 자동)
#    sudo bash deploy/openvpn-setup.sh add <client>   # 클라이언트 추가(.ovpn 생성)
#    sudo bash deploy/openvpn-setup.sh revoke <client># 클라이언트 폐기
#
#  웹 [VPN] 탭에서 추가/삭제 버튼을 쓰려면 이 스크립트에 대한 passwordless
#  sudo 를 허용하세요 (README 참고).
# ============================================================================
set -euo pipefail

SCRIPT_URL="https://raw.githubusercontent.com/angristan/openvpn-install/master/openvpn-install.sh"
WORK="/usr/local/lib/homelab-openvpn"
INSTALLER="$WORK/openvpn-install.sh"

if [ "$(id -u)" -ne 0 ]; then
  echo "root 권한이 필요합니다. 'sudo' 로 실행하세요." >&2
  exit 1
fi

mkdir -p "$WORK"
if [ ! -f "$INSTALLER" ]; then
  echo "설치 스크립트 내려받는 중..."
  curl -fsSL "$SCRIPT_URL" -o "$INSTALLER"
  chmod +x "$INSTALLER"
fi

cmd="${1:-install}"
case "$cmd" in
  install)
    if [ -e /etc/openvpn/server.conf ]; then
      echo "이미 설치되어 있습니다 (/etc/openvpn/server.conf)."
      exit 0
    fi
    echo "OpenVPN 서버를 기본값으로 설치합니다..."
    AUTO_INSTALL=y bash "$INSTALLER"
    # 연결 클라이언트 모니터링용 status 로그 활성화
    if [ -e /etc/openvpn/server.conf ] && ! grep -q '^status ' /etc/openvpn/server.conf; then
      echo "status /var/log/openvpn/status.log 10" >> /etc/openvpn/server.conf
      mkdir -p /var/log/openvpn
      systemctl restart openvpn-server@server 2>/dev/null || systemctl restart openvpn@server 2>/dev/null || true
    fi
    echo "완료. 생성된 .ovpn 파일을 웹 [VPN] 탭에서 내려받으세요."
    ;;
  add)
    name="${2:?클라이언트 이름이 필요합니다}"
    MENU_OPTION=1 CLIENT="$name" PASS=1 bash "$INSTALLER"
    ;;
  revoke)
    name="${2:?클라이언트 이름이 필요합니다}"
    MENU_OPTION=2 CLIENT="$name" bash "$INSTALLER"
    ;;
  *)
    echo "사용: $0 install | add <name> | revoke <name>" >&2
    exit 1
    ;;
esac
