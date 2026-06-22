#!/usr/bin/env bash
# ============================================================================
#  HomeLab Monitor 수동 업그레이드 스크립트
#  앱의 [설정] > 업데이트에서 "지금 업그레이드"를 누르면 동일한 작업이
#  자동으로 수행됩니다. 이 스크립트는 터미널에서 직접 업그레이드할 때 사용합니다.
#    bash deploy/upgrade.sh
# ============================================================================
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$APP_DIR"

echo "==> git fetch / pull (fast-forward)"
git fetch --all --prune
git pull --ff-only

echo "==> 의존성 설치"
if [ -x ".venv/bin/pip" ]; then
  .venv/bin/pip install -q -r requirements.txt
else
  python3 -m pip install -q -r requirements.txt
fi

echo "==> 재시작"
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet homelab-monitor; then
  sudo systemctl restart homelab-monitor
  echo "    systemd 서비스 재시작 완료"
else
  echo "    수동 실행 중이면 프로세스를 다시 시작하세요: python run.py"
fi

echo "완료!"
