#!/usr/bin/env bash
# ============================================================================
#  HomeLab Monitor systemd 서비스 자동 등록
#  현재 사용자/설치경로/가상환경 경로를 자동 감지해 서비스 파일을 생성합니다.
#    bash deploy/install-service.sh
# ============================================================================
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_USER="${SUDO_USER:-$(whoami)}"
PY="$APP_DIR/.venv/bin/python"

if [ ! -x "$PY" ]; then
  echo "⚠ 가상환경($PY)이 없습니다. 먼저 'bash deploy/install.sh' 를 실행하세요."
  exit 1
fi

echo "사용자  : $RUN_USER"
echo "설치경로: $APP_DIR"
echo "파이썬  : $PY"

sudo tee /etc/systemd/system/homelab-monitor.service >/dev/null <<EOF
[Unit]
Description=HomeLab Monitor (uptime / IP / Synology / router / speedtest)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$RUN_USER
WorkingDirectory=$APP_DIR
ExecStart=$PY $APP_DIR/run.py
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now homelab-monitor

echo
echo "✅ 등록 완료. 상태 확인:"
echo "   sudo systemctl status homelab-monitor"
