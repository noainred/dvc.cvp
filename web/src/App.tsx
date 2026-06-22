import { useMemo, useState } from 'react'
import { useLive } from './api'
import type { Device } from './types'
import { SummaryCards } from './components/SummaryCards'
import { DataCenterTable } from './components/DataCenterTable'
import { DeviceTable } from './components/DeviceTable'
import { DeviceView } from './views/DeviceView'
import { VersionBadge } from './components/VersionBadge'
import { AlertsBar } from './components/AlertsPanel'

type Route =
  | { view: 'overview' }
  | { view: 'dc'; id: string }
  | { view: 'device'; serial: string }

export default function App() {
  const { live, connected } = useLive()
  const [route, setRoute] = useState<Route>({ view: 'overview' })

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
          <VersionBadge />
        </div>
      </header>

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
