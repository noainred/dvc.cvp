import type { Device } from '../types'
import { bps, statusColor, statusLabel, uptime } from '../format'
import { UtilBadge } from './DataCenterTable'

interface Props {
  devices: Device[]
  onSelect: (serial: string) => void
  showDc?: boolean
}

// DeviceTable lists switches with role, model, port rollups and live traffic.
export function DeviceTable({ devices, onSelect, showDc }: Props) {
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>호스트명</th>
          {showDc && <th>DC</th>}
          <th>모델</th>
          <th>역할</th>
          <th>상태</th>
          <th>포트 (사용/미사용/오류)</th>
          <th>최대 사용률</th>
          <th>인입</th>
          <th>인출</th>
          <th>가동시간</th>
        </tr>
      </thead>
      <tbody>
        {devices.map((d) => (
          <tr key={d.serial} onClick={() => onSelect(d.serial)} className="clickable">
            <td>
              <div className="cell-title">{d.hostname}</div>
              <div className="cell-sub mono">{d.mgmtIp}</div>
            </td>
            {showDc && <td className="mono">{d.dataCenter}</td>}
            <td>
              <div>{d.model}</div>
              <div className="cell-sub">
                <span className={`fam-tag fam-${d.family}`}>{d.family}</span> {d.version}
              </div>
            </td>
            <td>{roleLabel(d.role)}</td>
            <td>
              <span className="status-dot" style={{ background: statusColor(d.status) }} />
              {statusLabel(d.status)}
            </td>
            <td>
              <span className="num used-text">{d.portUsed}</span> /{' '}
              <span className="num free-text">{d.portFree}</span> /{' '}
              <span className="num err-text">{d.portErr}</span>
              <span className="cell-sub"> · 총 {d.portTotal}</span>
            </td>
            <td>
              <UtilBadge util={d.maxUtilPct} />
            </td>
            <td className="mono">{bps(d.inBps)}</td>
            <td className="mono">{bps(d.outBps)}</td>
            <td className="cell-sub">{uptime(d.uptimeSec)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function roleLabel(role: string): string {
  switch (role) {
    case 'spine':
      return 'Spine'
    case 'leaf':
      return 'Leaf'
    case 'mgmt':
      return 'Mgmt'
    default:
      return role || '-'
  }
}
