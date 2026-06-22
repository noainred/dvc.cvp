# 🏠 HomeLab Monitor

라즈베리파이에서 동작하는 **자가 호스팅 홈 네트워크 모니터링** 웹 애플리케이션입니다.
집에서 쓰는 모든 장비의 **업타임/IP 관리**, **시놀로지 NAS 관리**, **공유기 관리**,
그리고 **인터넷 속도 측정**을 하나의 웹 화면에서 제공합니다.

업타임은 데이터베이스(SQLite)에 기록되며, **3년 이상 장기 보관·조회**가 가능하도록
설계되었습니다.

---

## ✨ 주요 기능

| 기능 | 설명 |
| --- | --- |
| **장비 업타임/IP 관리** | 모든 장비를 주기적으로 핑(ICMP)·TCP·HTTP 체크하고 가동률을 기록 |
| **장비 상태표 + 분/시간/날짜 조회** | 모든 장비의 IP 사용여부(live)를 표로 보고, IP 클릭 시 **분·시간·날짜** 단위로 업타임 조회 |
| **IP 스캔 (10분 주기)** | 서브넷을 스캔해 살아있는 호스트(IP/MAC/호스트명)를 발견·기록, 클릭 한 번으로 모니터링 장비 등록 |
| **장기 데이터 보관 (3년+)** | 원본 → 시간별 → **일별 집계(영구)** + **장애 이벤트 로그(영구)** 로 다운샘플링하여 DB를 작게 유지하면서 수년치 조회 가능 |
| **인터넷 속도 측정** | 주기적으로 다운로드/업로드/핑을 측정하여 기록·그래프화 (수동 측정 버튼도 제공) |
| **시놀로지 다중 NAS 관리** | 여러 대의 NAS를 웹에서 등록·관리, CPU/메모리/온도/볼륨 모니터링, **2단계 인증(OTP)** 지원 |
| **공유기 관리** | 연결 상태·지연 + (SNMP 사용 시) 가동시간·WAN 트래픽, 관리 페이지 바로가기 |
| **어디서나 접속 (Tailscale)** | Tailscale 연결 상태·접속 주소를 표시해 외부에서 안전하게 접속 |
| **로그인 (Google OTP)** | TOTP(Google Authenticator) 전용 로그인, **3회 실패 시 IP 차단**(정책 웹 제어), OS fail2ban 연동 |
| **버전 표시 / 자동 업그레이드** | 헤더·설정에 현재 버전(앱 버전+git 커밋) 표시, 웹에서 업데이트 확인·즉시 업그레이드, 주기적 자동 업데이트(옵션) |
| **웹 UI에서 모든 설정** | 장비·NAS·스캔·보안·측정주기·보관기간 등을 브라우저에서 관리 |

---

## 🏗️ 구조

```
backend/app/
├── main.py            FastAPI 앱 + 정적 프론트엔드 서빙 + 스케줄러 수명주기
├── config.py          config.yaml / 환경변수 로딩
├── database.py        SQLite(WAL) 엔진/세션
├── models.py          ORM 모델 (장비/체크/이벤트/집계/속도/시놀로지/설정)
├── scheduler.py       APScheduler 주기 작업 (핑/속도/시놀로지/집계/정리)
├── monitors/          ping · speedtest · synology · router(SNMP)
├── services/          uptime(기록/조회) · rollup(집계/보관) · settings
└── api/               REST 엔드포인트
frontend/              의존성 없는 SPA (vanilla JS + Chart.js)
deploy/                systemd 서비스 · 설치 스크립트
```

**기술 스택**: Python · FastAPI · SQLAlchemy · APScheduler · SQLite ·
Chart.js — 라즈베리파이(ARM)에서 단일 프로세스로 가볍게 동작합니다.

### 데이터 보관 전략 (3년 이상 조회)

| 테이블 | 내용 | 보관 |
| --- | --- | --- |
| `checks` | 원본 체크(1분 단위) | 기본 90일 (설정 가능) |
| `uptime_hourly` | 시간별 집계 | 기본 400일 |
| `uptime_daily` | **일별 집계** | **영구** |
| `events` | UP/DOWN 전환 이력 | **영구** |
| `speedtests` | 인터넷 속도 결과 | **영구** (소량) |

조회 범위에 따라 적절한 소스를 자동 선택합니다(단기=원본, 중기=시간별,
장기=일별). 일별 집계와 이벤트 로그는 영구 보관되므로 **3년 이상**이 지나도
가동률과 장애 시점을 조회할 수 있으며, 원본 데이터는 주기적으로 정리되어
DB 용량이 무한정 커지지 않습니다.

---

## 🚀 설치 (Raspberry Pi)

```bash
git clone <this-repo> homelab-monitor
cd homelab-monitor
bash deploy/install.sh           # python venv + 의존성 + config.yaml 생성
source .venv/bin/activate
python run.py                    # http://<라즈베리파이_IP>:8080
```

수동 설치:

```bash
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
cp config.example.yaml config.yaml   # 필요 시 수정
python run.py
```

### 부팅 시 자동 실행 (systemd)

```bash
sudo cp deploy/homelab-monitor.service /etc/systemd/system/
# 서비스 파일의 User / WorkingDirectory / ExecStart 경로 수정
sudo systemctl daemon-reload
sudo systemctl enable --now homelab-monitor
```

---

## ⚙️ 설정

- `config.yaml` : 부팅 기본값(서버 포트, DB 경로, 초기 장비 등). `config.example.yaml` 참고.
- **웹 [설정] 탭** : 측정 주기·보관 기간·시놀로지/공유기 연결을 실행 중에 변경 가능
  (DB에 저장되며 최대 30초 내 반영).

주요 환경변수: `HOMELAB_CONFIG`, `HOMELAB_HOST`, `HOMELAB_PORT`, `HOMELAB_DB`.

### 선택 의존성

```bash
pip install icmplib            # 더 빠른 네이티브 ICMP (없으면 system ping 사용)
pip install pysnmp-lextudio    # 공유기 SNMP 트래픽/가동시간 조회
# Ookla 공식 speedtest CLI 설치 시 자동 사용 (없으면 speedtest-cli 라이브러리)
```

### 시놀로지 연결

[설정] 탭에서 NAS 주소/포트(보통 5001, HTTPS)/계정/비밀번호 입력 후 **사용**을 켜세요.
관리자 권한 계정을 권장하며, 2단계 인증은 끄거나 전용 계정을 사용하세요.
(자체 서명 인증서면 `SSL 인증서 검증`을 끕니다.)

### 공유기 연결

기본은 핑 기반 연결 상태만 표시합니다. 공유기에서 **SNMP**를 켜면
(ipTIME 등 다수 지원) 가동시간·WAN 트래픽까지 조회할 수 있습니다.
[설정]에서 SNMP community와 WAN `ifIndex`를 지정하세요.

---

## 🔄 버전 표시 & 자동 업그레이드

- **버전 표시**: 헤더와 [설정] > 버전/자동 업그레이드에 현재 버전(`vX.Y.Z`)과 git
  커밋·브랜치가 표시됩니다. 새 버전이 있으면 헤더 버전 배지가 강조됩니다.
- **수동 업그레이드**: [설정]에서 **업데이트 확인** → **지금 업그레이드**를 누르면
  `git pull --ff-only` → `pip install -r requirements.txt` → **자동 재시작**이
  수행되고 진행 로그가 실시간으로 표시됩니다.
- **자동 업데이트(옵션)**: `update.auto_check` 로 주기적으로 새 버전을 확인하고,
  `update.auto_apply` 를 켜면 새 버전을 자동 설치·재시작합니다.

재시작 방식(우선순위):
1. `HOMELAB_RESTART_CMD` 환경변수가 있으면 그 명령 실행
   (예: `sudo systemctl restart homelab-monitor`, sudoers NOPASSWD 필요)
2. 없으면 현재 프로세스를 **그 자리에서 재실행**(`python run.py` 무중단 갱신)
3. systemd 서비스는 `Restart=always` 라 어떤 경우든 새 코드로 복구됩니다.

> ⚠️ 자동 업그레이드는 설정된 git 원격에서 코드를 받아 실행하므로, 신뢰하는
> 저장소에만 연결하세요. 끄려면 `update.allow_manual: false`, `auto_check: false`.

CLI로 업그레이드하려면: `bash deploy/upgrade.sh`

---

## 🔍 IP 스캔

서브넷(예: `192.168.0.0/24`)을 **10분마다** 스캔해 살아있는 호스트를 찾아 IP·MAC·
호스트명과 함께 기록합니다(미설정 시 라즈베리파이의 로컬 /24를 자동 감지). 발견한
호스트는 [IP 스캔] 탭에서 보고, **장비로 추가** 버튼으로 모니터링 대상에 바로 등록할
수 있습니다. MAC은 핑 후 커널 ARP 테이블(`ip neigh`)에서 읽으므로 root 권한이
필요 없습니다.

## 🔐 로그인 & 접속 차단 (Google OTP)

- **로그인**: 사용자 이름 + **TOTP(Google Authenticator) 6자리 코드**로만 로그인
  합니다(비밀번호 없음). [보안] 탭에서 계정을 만들고 QR을 스캔해 인증한 뒤
  "로그인 요구"를 켜세요. (계정이 없으면 켤 수 없어 잠김을 방지합니다.)
- **잠금 방지**: 로그인은 "활성 계정이 1개 이상 있을 때만" 실제로 적용됩니다.
  계정이 없으면(설정만 켜둔 상태) 잠기지 않고 열려 있어 계정을 만들 수 있습니다.
- **복구 (잠겼을 때)**: 라즈베리파이 터미널에서 관리 CLI로 즉시 해제할 수 있습니다.
  ```bash
  source .venv/bin/activate
  python manage.py disable-auth                 # 로그인 요구 끄기
  python manage.py add-user <이름> --enable      # 계정 바로 만들기(QR 출력)
  python manage.py list-users                   # 계정 목록
  ```
- **무차별 대입 차단(앱)**: 한 IP가 정해진 횟수(기본 3회) 이상 실패하면 해당 IP를
  지정 시간(기본 15분) 동안 차단합니다. 임계값·차단시간·집계창은 [보안] 탭에서 제어,
  차단 목록은 조회/해제할 수 있습니다 (`python manage.py bans` / `unban <ip>`).
- **OS 차단(fail2ban)**: SSH 등 OS 레벨 차단은 fail2ban과 연동합니다.
  설치: `sudo apt install fail2ban`. [보안] 탭에서 정책(maxretry/bantime/findtime)을
  저장하면 `/etc/fail2ban/jail.d/homelab-monitor.local`을 쓰고 reload를 시도합니다.

  > fail2ban 제어는 root 권한이 필요합니다. 앱(예: `pi` 사용자)에서 제어하려면
  > 아래처럼 passwordless sudo를 허용하세요(`sudo visudo`):
  > ```
  > pi ALL=(root) NOPASSWD: /usr/bin/fail2ban-client, /usr/bin/tee /etc/fail2ban/jail.d/homelab-monitor.local
  > ```
  > 권한이 없으면 적용할 정책 내용을 화면에 보여주어 수동 적용할 수 있게 합니다.

## 🌐 어디서나 접속 (Tailscale)

라즈베리파이에 Tailscale을 설치하면 외부에서도 모니터에 접속할 수 있습니다.

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

[원격접속] 탭에서 연결 상태와 **어디서나 접속 주소**(`http://100.x.y.z:8080`,
MagicDNS 이름)를 확인할 수 있습니다.

## 🔒 OpenVPN 서버

검증된 설치기(angristan/openvpn-install)를 래핑합니다. 설치는 root 상호작용이
필요하므로 터미널에서 1회 실행하세요:

```bash
sudo bash deploy/openvpn-setup.sh install        # 서버 설치(기본값 자동)
```

이후 웹 **[VPN] 탭**에서 서버 상태·접속 클라이언트·`.ovpn` 프로필 다운로드를
보고, 클라이언트 추가/폐기를 할 수 있습니다. 웹에서 추가/폐기 버튼을 쓰려면
해당 스크립트에 passwordless sudo 를 허용하세요(`sudo visudo`):

```
pi ALL=(root) NOPASSWD: /bin/bash /home/pi/homelab-monitor/deploy/openvpn-setup.sh
```

> 터미널로도 가능: `sudo bash deploy/openvpn-setup.sh add <이름>` /
> `... revoke <이름>`. 생성된 `.ovpn` 파일을 클라이언트 기기에 넣어 접속합니다.

## 🗄️ 시놀로지 2단계 인증(OTP)

NAS 계정에 2단계 인증이 켜져 있으면 일반 로그인이 거부됩니다(코드 403). [시놀로지] 탭에서
NAS를 등록할 때(또는 수정) **OTP 코드**를 1회 입력하면, 앱이 신뢰기기 토큰(device token)을
받아 저장하고 이후에는 OTP 없이 자동 폴링합니다. 2단계 인증을 쓰지 않는 전용 계정을
만들어도 됩니다.

---

## 🔌 주요 API

| 메서드 | 경로 | 설명 |
| --- | --- | --- |
| GET | `/api/devices/overview` | 전체 장비 현황 |
| GET/POST | `/api/devices` | 장비 목록 / 추가 |
| PATCH/DELETE | `/api/devices/{id}` | 수정 / 삭제 |
| GET | `/api/devices/{id}/uptime?range=3y` | 가동률·지연 시계열 |
| GET | `/api/devices/{id}/events` | 장비 장애 이력 |
| GET/POST | `/api/speedtest` · `/api/speedtest/run` | 속도 이력 / 즉시 측정 |
| GET | `/api/synology/status` · `/api/synology/history` | 시놀로지 상태/이력 |
| GET | `/api/router/status` | 공유기 상태 |
| GET/PUT | `/api/settings` | 런타임 설정 조회/변경 |
| GET | `/api/system/version` | 현재 버전/커밋 |
| GET | `/api/system/update-check` | 업데이트 확인 |
| POST | `/api/system/upgrade` | 즉시 업그레이드(+재시작) |
| GET | `/api/devices/{id}/uptime?granularity=minute\|hour\|day` | 분/시간/날짜 단위 업타임 |
| GET/POST | `/api/scan/hosts` · `/api/scan/run` | 발견 호스트 / 즉시 스캔 |
| GET/POST | `/api/synology/nas` | NAS 목록 / 추가 (다중) |
| GET | `/api/synology/nas/{id}/status` | NAS 실시간 상태 |
| GET | `/api/tailscale/status` | Tailscale 상태/접속 주소 |
| GET/POST | `/api/auth/state` · `/api/auth/login` | 인증 상태 / TOTP 로그인 |
| GET/POST/DELETE | `/api/auth/users` · `/api/auth/bans` | 계정 / IP 차단 관리 |
| GET/PUT | `/api/security/fail2ban/status` · `/policy` | fail2ban 상태/정책 |

대화형 API 문서: `http://<IP>:8080/docs`

---

## 📝 참고

- 단일 가정용 네트워크의 신뢰 환경을 가정합니다. 외부에 노출할 경우
  리버스 프록시(HTTPS)·인증을 앞단에 두는 것을 권장합니다.
- Chart.js는 CDN에서 로드합니다(라즈베리파이에 인터넷 연결 필요 — 속도
  측정 기능상 어차피 필요).
