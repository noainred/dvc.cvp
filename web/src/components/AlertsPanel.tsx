import { useState } from 'react'
import type { Alert } from '../types'

const typeLabel: Record<string, string> = {
  util: '사용률',
  optic: '광',
  errors: '에러',
  device: '장비',
  datacenter: 'DC'
}

// AlertsBar is a collapsible banner summarizing active alerts. It shows
// critical/warning counts and, when expanded, the alert list (most severe
// first). It is driven by the live SSE snapshot so it updates in real time.
export function AlertsBar({ alerts, onDevice }: { alerts: Alert[]; onDevice: (serial: string) => void }) {
  const [open, setOpen] = useState(false)
  if (!alerts || alerts.length === 0) {
    return <div className="alerts-bar ok">✓ 활성 경보 없음</div>
  }
  const crit = alerts.filter((a) => a.severity === 'critical').length
  const warn = alerts.length - crit

  return (
    <div className={`alerts-bar ${crit > 0 ? 'crit' : 'warn'}`}>
      <button className="alerts-head" onClick={() => setOpen((v) => !v)}>
        <span className="alerts-counts">
          {crit > 0 && <span className="badge crit">{crit} 심각</span>}
          {warn > 0 && <span className="badge warn">{warn} 경고</span>}
        </span>
        <span className="alerts-title">활성 경보 {alerts.length}건</span>
        <span className="alerts-toggle">{open ? '▲ 접기' : '▼ 펼치기'}</span>
      </button>
      {open && (
        <ul className="alerts-list">
          {alerts.slice(0, 100).map((a) => (
            <li
              key={a.key}
              className={`alert-row ${a.severity}${a.device ? ' clickable' : ''}`}
              onClick={() => a.device && onDevice(a.device)}
            >
              <span className={`alert-sev ${a.severity}`} />
              <span className="alert-type">{typeLabel[a.type] ?? a.type}</span>
              <span className="alert-msg">{a.message}</span>
              <span className="alert-since">{new Date(a.since).toLocaleTimeString()}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
