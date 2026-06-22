import type { Summary } from '../types'
import { bps } from '../format'

// SummaryCards shows the top-level fleet KPIs plus a port-usage breakdown bar.
export function SummaryCards({ s }: { s: Summary }) {
  const portUsedPct = s.portTotal ? (s.portUsed / s.portTotal) * 100 : 0
  return (
    <div className="summary">
      <Card label="데이터센터" value={`${s.dataCentersOk}/${s.dataCenters}`} sub="정상 / 전체" />
      <Card label="장비" value={`${s.devicesUp}/${s.devices}`} sub="가동 / 전체" />
      <Card label="총 인입" value={bps(s.totalInBps)} sub="실시간" accent="#38bdf8" />
      <Card label="총 인출" value={bps(s.totalOutBps)} sub="실시간" accent="#34d399" />
      <div className="card wide">
        <div className="card-label">포트 사용 현황 ({s.portTotal.toLocaleString()})</div>
        <div className="usage-bar">
          <span className="seg used" style={{ width: `${seg(s.portUsed, s.portTotal)}%` }} />
          <span className="seg free" style={{ width: `${seg(s.portFree, s.portTotal)}%` }} />
          <span className="seg disabled" style={{ width: `${seg(s.portDisabled, s.portTotal)}%` }} />
          <span className="seg error" style={{ width: `${seg(s.portError, s.portTotal)}%` }} />
        </div>
        <div className="usage-legend">
          <span><b className="dot used" />사용중 {s.portUsed.toLocaleString()} ({portUsedPct.toFixed(0)}%)</span>
          <span><b className="dot free" />미사용 {s.portFree.toLocaleString()}</span>
          <span><b className="dot disabled" />비활성 {s.portDisabled.toLocaleString()}</span>
          <span><b className="dot error" />오류 {s.portError.toLocaleString()}</span>
        </div>
      </div>
    </div>
  )
}

function Card({ label, value, sub, accent }: { label: string; value: string; sub?: string; accent?: string }) {
  return (
    <div className="card">
      <div className="card-label">{label}</div>
      <div className="card-value" style={accent ? { color: accent } : undefined}>{value}</div>
      {sub && <div className="card-sub">{sub}</div>}
    </div>
  )
}

function seg(v: number, total: number): number {
  return total ? (v / total) * 100 : 0
}
