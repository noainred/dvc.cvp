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
| **장기 데이터 보관 (3년+)** | 원본 → 시간별 → **일별 집계(영구)** + **장애 이벤트 로그(영구)** 로 다운샘플링하여 DB를 작게 유지하면서 수년치 조회 가능 |
| **업타임 그래프/조회** | 24시간·7일·30일·1년·3년 범위로 가동률·지연시간·장애 이력 조회 |
| **인터넷 속도 측정** | 주기적으로 다운로드/업로드/핑을 측정하여 기록·그래프화 (수동 측정 버튼도 제공) |
| **시놀로지 관리** | DSM API로 CPU/메모리/온도/가동시간/볼륨 사용량 모니터링 |
| **공유기 관리** | 연결 상태·지연 + (SNMP 사용 시) 가동시간·WAN 트래픽, 관리 페이지 바로가기 |
| **웹 UI에서 모든 설정** | 장비 추가/수정/삭제, 측정 주기, 보관 기간, 시놀로지/공유기 연결을 브라우저에서 관리 |

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

대화형 API 문서: `http://<IP>:8080/docs`

---

## 📝 참고

- 단일 가정용 네트워크의 신뢰 환경을 가정합니다. 외부에 노출할 경우
  리버스 프록시(HTTPS)·인증을 앞단에 두는 것을 권장합니다.
- Chart.js는 CDN에서 로드합니다(라즈베리파이에 인터넷 연결 필요 — 속도
  측정 기능상 어차피 필요).
