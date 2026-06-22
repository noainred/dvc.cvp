# dvc.cvp — Arista CloudVision 통합 관제

한국 본사에서 전 세계 20+ 데이터센터의 **Arista 스위치(7280 / 7504 / 7010)** 를
실시간으로 관제·모니터링하는 웹 프로그램입니다. 데이터센터 인벤토리·토폴로지는
**CloudVision Portal(CVP) API** 로, 포트별 실시간 트래픽·사용률은 CVP 에서 학습한
장비 주소로 **EOS eAPI** 를 통해 수집하며, 원격지 연결은 모두 사이트별
**SSH 점프호스트(bastion)** 를 경유합니다.

> 인프라 없이도 바로 확인할 수 있도록 **데모 모드**(합성 데이터)가 기본 내장되어 있습니다.

---

## 주요 기능

- **전체 현황 대시보드** — 데이터센터/장비/포트 KPI, 포트 사용 현황(사용중·미사용·비활성·오류), 패밀리(7280/7504/7010)별 집계, 트래픽 상위 장비 Top 10
- **실시간 포트 사용률** — 인터페이스별 인입/인출 bps 와 링크 속도 대비 사용률(%) 계산, SSE 로 실시간 갱신
- **포트 사용/미사용 시각화** — 장비별 포트 그리드 히트맵. 사용중 포트는 사용률에 따라 녹색→적색, 미사용/비활성/오류 포트는 색상으로 구분
- **트래픽 추이 차트** — 장비 전체 및 포트별 인입/인출 시계열 그래프
- **다중 데이터센터 관제** — 사이트별 상태(정상/저하/연결불가), 프록시 방식, 장비·포트·트래픽 롤업
- **프록시 연결** — 사이트별 SSH 점프호스트를 통한 터널링(직접 연결 `direct` 도 지원)

## 아키텍처

```
                         ┌──────────────────────────────────────────┐
                         │      한국 본사 (dvc.cvp 수집기/웹서버)       │
                         │                                          │
   브라우저 ◀── SSE/REST ──▶│  Go 백엔드  ──▶  Collector  ──▶  Store    │
   (React 대시보드)         │     │              │   (in-memory 시계열) │
                         │     └─ 정적 서빙(web/dist)                 │
                         └───────────┬──────────────────────────────┘
                                     │ 사이트별 SSH 점프호스트(터널)
            ┌────────────────────────┼────────────────────────┐
            ▼                        ▼                        ▼
   ┌─────────────────┐     ┌─────────────────┐      ┌─────────────────┐
   │  DC: Seoul HQ    │     │  DC: Ashburn     │ ...  │  DC: (20+)       │
   │  CVP REST(인벤토리)│    │  CVP REST        │      │                 │
   │  eAPI(카운터)     │     │  eAPI            │      │                 │
   │  7280/7504/7010  │     │  7280/7504/7010  │      │                 │
   └─────────────────┘     └─────────────────┘      └─────────────────┘
```

- **CVP REST API** (`/cvpservice/inventory/devices` 등): 장비 인벤토리·모델·버전·스트리밍 상태·관리 IP 수집
- **EOS eAPI** (`show interfaces`): 인터페이스 상태/속도/옥텟 카운터 수집 → 폴링 간 델타로 bps·사용률 계산
- **프록시**: 위 모든 HTTP 호출은 사이트별 SSH 점프호스트 터널을 통해 다이얼됩니다(`internal/proxy`)

> 수집 백엔드는 `Provider` 인터페이스로 추상화되어 있어(`internal/collector`),
> 실 CVP(`internal/cvp`) 와 데모 생성기(`internal/collector/mock.go`) 가 동일한
> 레이트 계산·포트 분류 경로를 공유합니다.

## 빠른 시작 (데모 모드)

인프라 없이 합성 데이터로 전체 UI 를 확인합니다.

```bash
# 1) 프론트엔드 빌드
cd web && npm install && npm run build && cd ..

# 2) 백엔드 빌드 & 실행 (config 파일이 없으면 자동으로 demo 모드)
go build -o bin/dvc-cvp ./cmd/server
./bin/dvc-cvp -listen :8080
```

또는 `make build && make run`. 브라우저에서 <http://localhost:8080> 접속.

개발 시에는 두 프로세스를 분리해 실행할 수 있습니다.

```bash
make dev-backend     # Go 서버 :8080 (demo)
make dev-frontend    # Vite :5173, /api 는 :8080 으로 프록시
```

## 실 운영 모드 (CloudVision)

```bash
cp config/config.example.yaml config/config.yaml
# config.yaml 에서 mode: cvp 로 변경하고 데이터센터/CVP/프록시 정보 입력
make build
./bin/dvc-cvp -config config/config.yaml
```

`config/config.yaml` 핵심 항목:

| 항목 | 설명 |
|------|------|
| `mode` | `demo` 또는 `cvp` |
| `datacenters[].cvp.url` | 사이트 CloudVision Portal 주소 |
| `datacenters[].cvp.token` / `username`+`password` | CVP 인증(서비스 계정 토큰 권장) |
| `datacenters[].cvp.deviceUsername` / `devicePassword` | 스위치 eAPI 자격증명 |
| `datacenters[].proxy.type` | `ssh`(점프호스트) 또는 `direct` |
| `datacenters[].proxy.jumpHost` / `user` / `keyFile` | SSH 터널 설정 |

전체 예시는 [`config/config.example.yaml`](config/config.example.yaml) 참고.

## API

| 메서드 · 경로 | 설명 |
|---------------|------|
| `GET /api/health` | 헬스체크 + 모드 |
| `GET /api/summary` | 전체 요약(KPI·포트 집계·패밀리별) |
| `GET /api/datacenters` | 데이터센터 목록·롤업 |
| `GET /api/datacenters/{id}/devices` | 특정 DC 장비 |
| `GET /api/devices?dc=` | 장비 목록(옵션: DC 필터) |
| `GET /api/devices/{serial}` | 장비 상세 |
| `GET /api/devices/{serial}/interfaces` | 인터페이스 목록(상태·속도·bps·사용률) |
| `GET /api/devices/{serial}/history` | 장비 전체 트래픽 시계열 |
| `GET /api/devices/{serial}/interface-history?name=Ethernet1` | 인터페이스 트래픽 시계열 |
| `GET /api/stream` | **SSE** 실시간 스냅샷 스트림 |

## 프로젝트 구조

```
cmd/server/          진입점(설정 로드·와이어링·HTTP 서버)
internal/
  config/            YAML 설정 로드·검증
  model/             도메인 타입(DataCenter/Device/Interface/…)
  proxy/             SSH 점프호스트 / direct 다이얼러
  cvp/               CloudVision REST 클라이언트 + eAPI + Provider
  collector/         폴링 루프·레이트 계산·포트 분류 + 데모 생성기(mock)
  store/             동시성 안전 인메모리 캐시 + 시계열 링버퍼
  api/               REST 핸들러·SSE 허브·정적 서빙
web/                 React + TypeScript + Vite 대시보드 (의존성 없는 SVG 차트)
config/              설정 예시
```

## 포트 상태 분류

| 상태 | 기준 | 의미 |
|------|------|------|
| `used` (사용중) | oper `connected`/`up` | 링크가 연결되어 사용 중 |
| `free` (미사용) | admin up + oper `notconnect` | 패치/가용 대기, 미사용 |
| `disabled` (비활성) | admin `down` | 관리상 셧다운 |
| `error` (오류) | oper `errdisabled` | err-disabled 등 오류 상태 |

## 빌드/테스트

```bash
make build      # 프론트엔드 + 백엔드
make test       # go test ./...
make tidy       # go mod tidy
make clean      # bin/ web/dist 정리
```

## 참고/보안

- 스위치·CVP 인증서는 자가서명인 경우가 많아 `insecureSkipVerify: true` 기본 예시를 제공합니다. 운영 시 신뢰 CA 사용을 권장합니다.
- SSH `knownHosts` 미설정 시 호스트키 검증을 생략합니다(데모 편의). 운영 시 `known_hosts` 지정을 권장합니다.
- `config/config.yaml` 에는 자격증명이 포함되므로 `.gitignore` 로 커밋에서 제외됩니다.
- 실시간 카운터를 CVP 스트리밍 텔레메트리(Resource API)로 대체하려면 `internal/cvp` 의 인터페이스 수집부를 교체하면 됩니다.
