import { useState } from 'react'
import type { Congestion, DataCenter, Flow, ReclaimPort, Topology, TopologyNode } from '../types'
import { api, usePolling } from '../api'
import { bps, speed, statusColor } from '../format'

// DcSelect is a shared data-center filter dropdown.
function DcSelect({
  datacenters,
  value,
  onChange,
  allowAll = true
}: {
  datacenters: DataCenter[]
  value: string
  onChange: (v: string) => void
  allowAll?: boolean
}) {
  return (
    <select className="dc-select" value={value} onChange={(e) => onChange(e.target.value)}>
      {allowAll && <option value="">전체 데이터센터</option>}
      {datacenters.map((d) => (
        <option key={d.id} value={d.id}>
          {d.name}
        </option>
      ))}
    </select>
  )
}

// FlowsView — sFlow/IPFIX top talkers.
export function FlowsView({ datacenters }: { datacenters: DataCenter[] }) {
  const [dc, setDc] = useState('')
  const { data } = usePolling<Flow[]>(() => api.flows(dc, 50), [dc], 5000)
  const rows = data ?? []
  return (
    <section className="panel">
      <div className="panel-head">
        <h3>Top Talkers <span className="cell-sub">(sFlow/IPFIX · 데모 데이터)</span></h3>
        <DcSelect datacenters={datacenters} value={dc} onChange={setDc} />
      </div>
      <table className="data-table">
        <thead>
          <tr><th>장비</th><th>출발지</th><th>목적지</th><th>App</th><th>Proto/Port</th><th>대역폭</th></tr>
        </thead>
        <tbody>
          {rows.map((f, i) => (
            <tr key={i}>
              <td className="cell-title">{f.hostname}</td>
              <td className="mono">{f.srcIp}</td>
              <td className="mono">{f.dstIp}</td>
              <td>{f.app}</td>
              <td className="mono">{f.proto}/{f.port}</td>
              <td className="mono">{bps(f.bps)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length === 0 && <p className="hint">데이터 수집 중…</p>}
    </section>
  )
}

// CongestionView — LANZ microburst/queue congestion.
export function CongestionView({ datacenters }: { datacenters: DataCenter[] }) {
  const [dc, setDc] = useState('')
  const { data } = usePolling<Congestion[]>(() => api.congestion(dc, 100), [dc], 5000)
  const rows = data ?? []
  return (
    <section className="panel">
      <div className="panel-head">
        <h3>혼잡 / 마이크로버스트 <span className="cell-sub">(LANZ · 데모 데이터)</span></h3>
        <DcSelect datacenters={datacenters} value={dc} onChange={setDc} />
      </div>
      <table className="data-table">
        <thead>
          <tr><th>장비</th><th>포트</th><th>큐 깊이</th><th>지속</th><th>드롭</th><th>심각도</th><th>시각</th></tr>
        </thead>
        <tbody>
          {rows.map((c, i) => (
            <tr key={i}>
              <td className="cell-title">{c.hostname}</td>
              <td className="mono">{c.interface}</td>
              <td className="mono">{c.queueDepthKb.toLocaleString()} KB</td>
              <td className="mono">{c.durationMs} ms</td>
              <td className="mono err-text">{c.drops}</td>
              <td><span className={`sev-tag ${c.severity}`}>{c.severity === 'critical' ? '심각' : '경고'}</span></td>
              <td className="cell-sub">{new Date(c.time).toLocaleTimeString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length === 0 && <p className="hint">현재 감지된 혼잡 이벤트 없음.</p>}
    </section>
  )
}

// ReclaimView — idle free ports that can be reclaimed.
export function ReclaimView() {
  const [days, setDays] = useState(30)
  const { data } = usePolling<ReclaimPort[]>(() => api.reclaim(days), [days], 10000)
  const rows = data ?? []
  const ready = rows.filter((r) => r.hasTransceiver).length
  return (
    <section className="panel">
      <div className="panel-head">
        <h3>포트 회수 대상 <span className="cell-sub">({days}일 이상 미사용 free 포트)</span></h3>
        <select className="dc-select" value={days} onChange={(e) => setDays(Number(e.target.value))}>
          {[7, 30, 90, 180, 365].map((d) => <option key={d} value={d}>{d}일 이상</option>)}
        </select>
      </div>
      <p className="hint">총 {rows.length}개 · 모듈 장착(재사용 가능) {ready}개 / 모듈 없음 {rows.length - ready}개</p>
      <table className="data-table">
        <thead>
          <tr><th>장비</th><th>DC</th><th>포트</th><th>속도</th><th>모듈</th><th>유휴기간</th><th>마지막 변경</th></tr>
        </thead>
        <tbody>
          {rows.slice(0, 300).map((r, i) => (
            <tr key={i}>
              <td className="cell-title">{r.hostname}</td>
              <td className="mono">{r.dataCenter}</td>
              <td className="mono">{r.interface}</td>
              <td className="mono">{speed(r.speedBps)}</td>
              <td>{r.hasTransceiver ? <span className="ok-text">장착 {r.mediaType}</span> : <span className="cell-sub">없음</span>}</td>
              <td className="mono">{Math.round(r.idleDays)}일</td>
              <td className="cell-sub">{new Date(r.lastChange).toLocaleDateString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

// ComplianceView — EOS version inventory + configuration compliance.
export function ComplianceView({ onDevice }: { onDevice: (serial: string) => void }) {
  const { data } = usePolling(() => api.compliance(), [], 10000)
  if (!data) return <p className="hint">로딩 중…</p>
  return (
    <>
      <section className="panel">
        <h3>EOS 버전 인벤토리 <span className="cell-sub">(총 {data.total}대 · 비준수 {data.nonCompliant}대)</span></h3>
        <table className="data-table">
          <thead><tr><th>EOS 버전</th><th>대수</th><th>비준수</th></tr></thead>
          <tbody>
            {data.byVersion.map((v) => (
              <tr key={v.version}>
                <td className="mono">{v.version}</td>
                <td>{v.count}</td>
                <td className={v.nonCompliant > 0 ? 'err-text' : ''}>{v.nonCompliant}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      <section className="panel">
        <h3>구성 비준수 장비 ({data.devices.length})</h3>
        <table className="data-table">
          <thead><tr><th>호스트명</th><th>DC</th><th>모델</th><th>EOS</th></tr></thead>
          <tbody>
            {data.devices.map((d) => (
              <tr key={d.serial} className="clickable" onClick={() => onDevice(d.serial)}>
                <td className="cell-title">{d.hostname}</td>
                <td className="mono">{d.dataCenter}</td>
                <td>{d.model}</td>
                <td className="mono">{d.version}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {data.devices.length === 0 && <p className="hint">모든 장비가 구성 준수 상태입니다. ✓</p>}
      </section>
    </>
  )
}

// TopologyView — LLDP-derived spine/leaf graph for one data center.
export function TopologyView({ datacenters }: { datacenters: DataCenter[] }) {
  const [dc, setDc] = useState(datacenters[0]?.id ?? '')
  const { data } = usePolling<Topology>(() => api.topology(dc || datacenters[0]?.id || ''), [dc], 10000)
  const W = 920, H = 380
  const layerY: Record<string, number> = { spine: 70, leaf: 200, mgmt: 320 }
  const nodes = data?.nodes ?? []
  const pos = new Map<string, { x: number; y: number; n: TopologyNode }>()
  const layers: Record<string, TopologyNode[]> = { spine: [], leaf: [], mgmt: [] }
  for (const n of nodes) (layers[n.role] ?? layers.mgmt).push(n)
  for (const role of Object.keys(layers)) {
    const arr = layers[role]
    arr.forEach((n, i) => {
      pos.set(n.id, { x: ((i + 1) / (arr.length + 1)) * W, y: layerY[role] ?? 320, n })
    })
  }
  return (
    <section className="panel">
      <div className="panel-head">
        <h3>토폴로지 (LLDP) <span className="cell-sub">spine ▲ / leaf / mgmt</span></h3>
        <DcSelect datacenters={datacenters} value={dc} onChange={setDc} allowAll={false} />
      </div>
      {nodes.length === 0 ? (
        <p className="hint">토폴로지 데이터 없음.</p>
      ) : (
        <svg viewBox={`0 0 ${W} ${H}`} width="100%" className="topo">
          {(data?.links ?? []).map((l, i) => {
            const a = pos.get(l.a), b = pos.get(l.b)
            if (!a || !b) return null
            return (
              <line key={i} x1={a.x} y1={a.y} x2={b.x} y2={b.y}
                className="topo-link" strokeWidth={Math.min(1 + l.count, 5)} />
            )
          })}
          {[...pos.values()].map(({ x, y, n }) => (
            <g key={n.id} transform={`translate(${x},${y})`}>
              <circle r={12} fill={statusColor(n.status)} stroke="#0b1220" strokeWidth={2} />
              <text y={26} textAnchor="middle" className="topo-label">{n.hostname.replace(dc + '-', '')}</text>
            </g>
          ))}
        </svg>
      )}
    </section>
  )
}
