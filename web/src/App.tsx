import { useEffect, useMemo, useState } from 'react'
import { auth, useLive } from './api'
import { Login } from './components/Login'
import type { Device } from './types'
import { SummaryCards } from './components/SummaryCards'
import { DataCenterTable } from './components/DataCenterTable'
import { DeviceTable } from './components/DeviceTable'
import { DeviceView } from './views/DeviceView'
import { VersionBadge } from './components/VersionBadge'
import { AlertsBar } from './components/AlertsPanel'
import { TrendPanel } from './components/TrendPanel'
import {
  AuditView,
  ComplianceView,
  CongestionView,
  FlowsView,
  ReclaimView,
  TopologyView
} from './views/MoreViews'

type Route =
  | { view: 'overview' }
  | { view: 'dc'; id: string }
  | { view: 'device'; serial: string }
  | { view: 'topology' }
  | { view: 'flows' }
  | { view: 'congestion' }
  | { view: 'reclaim' }
  | { view: 'compliance' }
  | { view: 'audit' }

interface AuthState {
  enabled: boolean
  role: string
  username?: string
}

const NAV: { key: Route['view']; label: string }[] = [
  { key: 'overview', label: '현황' },
  { key: 'topology', label: '토폴로지' },
  { key: 'flows', label: 'Top Talkers' },
  { key: 'congestion', label: '혼잡(LANZ)' },
  { key: 'reclaim', label: '포트 회수' },
  { key: 'compliance', label: '컴플라이언스' }
]

export default function App() {
  const { live, connected } = useLive()
  const [route, setRoute] = useState<Route>({ view: 'overview' })
  const [authState, setAuthState] = useState<AuthState | null>(null)

  useEffect(() => {
    auth.status().then(setAuthState).catch(() => setAuthState({ enabled: false, role: 'admin' }))
  }, [])
  const refreshAuth = () => auth.status().then(setAuthState).catch(() => {})

  if (authState && authState.enabled && !authState.role) {
    return <Login onSuccess={refreshAuth} />
  }
  const role = authState?.role || 'admin'
  const tabs = role === 'admin' ? [...NAV, { key: 'audit' as Route['view'], label: '감사로그' }] : NAV

  const devices = live?.devices ?? []
  const datacenters = live?.datacenters ?? []

  const currentDc = route.view === 'dc' ? datacenters.find((d) => d.id === route.id) : undefined
  const currentDevice: Device | null =
    route.view === 'device' ? devices.find((d) => d.serial === route.serial) ?? null : null

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand" onClick={() => setRoute({ view: 'overview' })}>
          <span className="brand-mark">◢◤</span>
          <span className="brand-name">dvc.cvp</span>
          <span className="brand-sub">Arista CloudVision 통합 관제</span>
        </div>
        <div className="topbar-right">
          <span className={`conn ${connected ? 'on' : 'off'}`}>
            <span className="conn-dot" /> {connected ? '실시간 연결됨' : '연결 끊김'}
          </span>
          {live && (
            <span className="updated">
              갱신 {new Date(live.summary.updatedAt).toLocaleTimeString()}
            </span>
          )}
          {authState?.enabled && (
            <span className="user-box">
              {authState.username}
              <button
                className="logout-btn"
                onClick={async () => {
                  await auth.logout()
                  setRoute({ view: 'overview' })
                  refreshAuth()
                }}
              >
                로그아웃
              </button>
            </span>
          )}
          <VersionBadge admin={role === 'admin'} />
        </div>
      </header>

      <nav className="tabs">
        {tabs.map((t) => (
          <button
            key={t.key}
            className={`tab${route.view === t.key ? ' active' : ''}`}
            onClick={() => setRoute({ view: t.key } as Route)}
          >
            {t.label}
          </button>
        ))}
      </nav>

      <nav className="breadcrumb">
        <a onClick={() => setRoute({ view: 'overview' })}>전체 현황</a>
        {route.view === 'dc' && currentDc && <span> › {currentDc.name}</span>}
        {route.view === 'device' && (
          <>
            {currentDevice && (
              <a onClick={() => setRoute({ view: 'dc', id: currentDevice.dataCenter })}>
                {' › '}
                {datacenters.find((d) => d.id === currentDevice.dataCenter)?.name ??
                  currentDevice.dataCenter}
              </a>
            )}
            <span> › {currentDevice?.hostname ?? route.serial}</span>
          </>
        )}
      </nav>

      <main className="content">
        {!live && <div className="loading">CloudVision 텔레메트리 연결 중…</div>}

        {live && (
          <AlertsBar
            alerts={live.alerts}
            onDevice={(serial) => setRoute({ view: 'device', serial })}
          />
        )}

        {live && route.view === 'overview' && (
          <Overview
            live={live}
            onDc={(id) => setRoute({ view: 'dc', id })}
            onDevice={(serial) => setRoute({ view: 'device', serial })}
          />
        )}

        {live && route.view === 'dc' && currentDc && (
          <section>
            <h2>{currentDc.name} <span className="cell-sub">· {currentDc.city} · {currentDc.region}</span></h2>
            {currentDc.error && <div className="error-banner">연결 오류: {currentDc.error}</div>}
            <DeviceTable
              devices={devices.filter((d) => d.dataCenter === currentDc.id)}
              onSelect={(serial) => setRoute({ view: 'device', serial })}
            />
          </section>
        )}

        {live && route.view === 'device' && (
          <DeviceView serial={route.serial} device={currentDevice} />
        )}

        {live && route.view === 'topology' && <TopologyView datacenters={datacenters} />}
        {live && route.view === 'flows' && <FlowsView datacenters={datacenters} />}
        {live && route.view === 'congestion' && <CongestionView datacenters={datacenters} />}
        {live && route.view === 'reclaim' && <ReclaimView />}
        {live && route.view === 'compliance' && (
          <ComplianceView onDevice={(serial) => setRoute({ view: 'device', serial })} />
        )}
        {live && route.view === 'audit' && <AuditView />}
      </main>
    </div>
  )
}

function Overview({
  live,
  onDc,
  onDevice
}: {
  live: NonNullable<ReturnType<typeof useLive>['live']>
  onDc: (id: string) => void
  onDevice: (serial: string) => void
}) {
  const topTalkers = useMemo(
    () => [...live.devices].sort((a, b) => b.inBps + b.outBps - (a.inBps + a.outBps)).slice(0, 10),
    [live.devices]
  )
  return (
    <>
      <SummaryCards s={live.summary} />

      <div className="family-row">
        {live.summary.byFamily.map((f) => (
          <span key={f.family} className={`fam-tag fam-${f.family} fam-big`}>
            Arista {f.family} · {f.count}대
          </span>
        ))}
      </div>

      <TrendPanel />

      <section className="panel">
        <h3>데이터센터 ({live.datacenters.length})</h3>
        <DataCenterTable datacenters={live.datacenters} onSelect={onDc} />
      </section>

      <section className="panel">
        <h3>트래픽 상위 장비 (Top 10)</h3>
        <DeviceTable devices={topTalkers} onSelect={onDevice} showDc />
      </section>
    </>
  )
}
