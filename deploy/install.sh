#!/usr/bin/env bash
# ============================================================================
#  HomeLab Monitor 설치 스크립트 (Raspberry Pi OS / Debian / Ubuntu)
#  사용: bash deploy/install.sh
# ============================================================================
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$APP_DIR"

echo "==> 1/5 시스템 패키지 확인 (python3, venv, iputils-ping, iproute2)"
if command -v apt-get >/dev/null 2>&1; then
  sudo apt-get update -qq
  # iputils-ping: 핑 체크 / iproute2: IP 스캔 시 ARP(MAC) 조회(ip neigh)
  sudo apt-get install -y python3 python3-venv python3-pip iputils-ping iproute2
  # (선택) OS 로그인 차단: fail2ban — 웹 [보안] 탭에서 정책 제어
  #   sudo apt-get install -y fail2ban
fi

echo "==> 2/5 파이썬 가상환경 생성 (.venv)"
python3 -m venv .venv
# shellcheck disable=SC1091
source .venv/bin/activate

echo "==> 3/5 의존성 설치"
pip install --upgrade pip
pip install -r requirements.txt
# 선택: 더 빠른 ICMP / 공유기 SNMP
# pip install icmplib pysnmp-lextudio

echo "==> 4/5 설정 파일 준비"
if [ ! -f config.yaml ]; then
  cp config.example.yaml config.yaml
  echo "    config.yaml 생성됨 — 환경에 맞게 수정하세요."
fi
mkdir -p data

echo "==> 5/5 (선택) systemd 서비스 등록"
echo "    아래 명령으로 부팅 시 자동 실행을 설정할 수 있습니다:"
echo
echo "    sudo cp deploy/homelab-monitor.service /etc/systemd/system/"
echo "    # 서비스 파일의 User/WorkingDirectory/ExecStart 경로를 환경에 맞게 수정"
echo "    sudo systemctl daemon-reload"
echo "    sudo systemctl enable --now homelab-monitor"
echo
echo
echo "선택 기능:"
echo "  - 어디서나 접속(Tailscale):  curl -fsSL https://tailscale.com/install.sh | sh && sudo tailscale up"
echo "  - OpenVPN 서버 설치:         sudo bash deploy/openvpn-setup.sh install"
echo "  - OS 로그인 차단(fail2ban):  sudo apt-get install -y fail2ban"
echo "  - 공유기 SNMP / 빠른 ICMP:   pip install pysnmp-lextudio icmplib"
echo
echo "완료! 바로 실행하려면:  source .venv/bin/activate && python run.py"
echo "웹 접속:  http://<라즈베리파이_IP>:8080"
