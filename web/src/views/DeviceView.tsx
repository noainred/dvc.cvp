import { useEffect, useState } from 'react'
import type { Device, Interface } from '../types'
import { api, usePolling } from '../api'
import { bps, pct, speed, stateLabel, statusColor, statusLabel, uptime } from '../format'
import { PortGrid } from '../components/PortGrid'
import { TrafficChart } from '../components/TrafficChart'

interface Props {
  serial: string
  device: Device | null
}

// DeviceView is the per-switch drill-down: device traffic chart, the port grid
// heatmap, and an on-demand per-port traffic chart.
export function DeviceView({ serial, device }: Props) {
  const [selected, setSelected] = useState<string | null>(null)
  const { data: interfaces } = usePolling<Interface[]>(() => api.interfaces(serial), [serial], 5000)
  const { data: devHistory } = usePolling(() => api.deviceHistory(serial), [serial], 5000)

  // Reset port selection when switching devices.
  useEffect(() => setSelected(null), [serial])

  const sel = interfaces?.find((i) => i.name === selected) ?? null

  return (
    <div>
      {device && (
        <div className="device-head">
          <div>
            <h2>{device.hostname}</h2>
            <div className="device-meta">
              <span><span className="status-dot" style={{ background: statusColor(device.status) }} />{statusLabel(device.status)}</span>
              <span className={`fam-tag fam-${device.family}`}>{device.family}</span>
              <span>{device.model}</span>
              <span className="mono">{device.mgmtIp}</span>
              <span>EOS {device.version}</span>
              <span>가동 {uptime(device.uptimeSec)}</span>
            </div>
          </div>
          <div className="device-kpis">
            <KPI label="포트" value={`${device.portUsed}/${device.portTotal}`} sub="사용/총" />
            <KPI label="인입" value={bps(device.inBps)} />
            <KPI label="인출" value={bps(device.outBps)} />
            <KPI label="최대사용률" value={pct(device.maxUtilPct)} />
          </div>
        </div>
      )}

      <section className="panel">
        <h3>장비 전체 트래픽</h3>
        <TrafficChart samples={devHistory ?? []} />
      </section>

      <section className="panel">
        <h3>포트 사용 현황 {interfaces ? `(${interfaces.length})` : ''}</h3>
        <p className="hint">사용중 포트는 사용률에 따라 녹색→적색으로 표시됩니다. 포트를 클릭하면 상세 트래픽을 볼 수 있습니다.</p>
        {interfaces ? (
          <PortGrid interfaces={interfaces} selected={selected} onSelect={setSelected} />
        ) : (
          <div className="chart-empty">인터페이스 로딩 중…</div>
        )}
      </section>

      {sel && <PortDetail serial={serial} iface={sel} onClose={() => setSelected(null)} />}
    </div>
  )
}

function PortDetail({ serial, iface, onClose }: { serial: string; iface: Interface; onClose: () => void }) {
  const { data: hist } = usePolling(() => api.interfaceHistory(serial, iface.name), [serial, iface.name], 5000)
  return (
    <section className="panel port-detail">
      <div className="panel-head">
        <h3>{iface.name} {iface.description && <span className="cell-sub">— {iface.description}</span>}</h3>
        <button className="btn-close" onClick={onClose}>닫기 ✕</button>
      </div>
      <div className="port-detail-grid">
        <PortStat label="상태" value={`${stateLabel(iface.state)} (${iface.adminStatus}/${iface.operStatus})`} />
        <PortStat label="속도" value={speed(iface.speedBps)} />
        <PortStat label="사용률" value={pct(iface.utilPct)} />
        <PortStat label="인입 / 인출" value={`${bps(iface.inBps)} / ${bps(iface.outBps)}`} />
        <PortStat label="오류 (In/Out)" value={`${iface.inErrors} / ${iface.outErrors}`} />
        <PortStat label="폐기 (In/Out)" value={`${iface.inDiscards} / ${iface.outDiscards}`} />
        {iface.neighbor && <PortStat label="이웃(LLDP)" value={iface.neighbor} />}
      </div>
      <TrafficChart samples={hist ?? []} height={180} />
    </section>
  )
}

function KPI({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="kpi">
      <div className="kpi-label">{label}</div>
      <div className="kpi-value">{value}</div>
      {sub && <div className="card-sub">{sub}</div>}
    </div>
  )
}

function PortStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="port-stat">
      <span className="port-stat-label">{label}</span>
      <span className="port-stat-value">{value}</span>
    </div>
  )
}
