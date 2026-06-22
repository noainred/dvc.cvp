import type { DataCenter } from '../types'
import { bps, pct, statusColor, statusLabel, utilColor } from '../format'

interface Props {
  datacenters: DataCenter[]
  onSelect: (id: string) => void
}

// DataCenterTable lists every site with health, port usage and live traffic.
export function DataCenterTable({ datacenters, onSelect }: Props) {
  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>데이터센터</th>
          <th>지역</th>
          <th>상태</th>
          <th>프록시</th>
          <th>장비</th>
          <th>포트 (사용/미사용)</th>
          <th>최대 사용률</th>
          <th>인입</th>
          <th>인출</th>
        </tr>
      </thead>
      <tbody>
        {datacenters.map((dc) => (
          <tr key={dc.id} onClick={() => onSelect(dc.id)} className="clickable">
            <td>
              <div className="cell-title">{dc.name}</div>
              <div className="cell-sub">{dc.city}</div>
            </td>
            <td>{dc.region}</td>
            <td>
              <span className="status-dot" style={{ background: statusColor(dc.status) }} />
              {statusLabel(dc.status)}
            </td>
            <td className="mono">{dc.proxyType}</td>
            <td>
              {dc.deviceUp}/{dc.deviceCount}
            </td>
            <td>
              <span className="num used-text">{dc.portUsed}</span> /{' '}
              <span className="num free-text">{dc.portFree}</span>
              <span className="cell-sub"> · 총 {dc.portTotal}</span>
            </td>
            <td>
              <UtilBadge util={dc.maxUtilPct} />
            </td>
            <td className="mono">{bps(dc.inBps)}</td>
            <td className="mono">{bps(dc.outBps)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function UtilBadge({ util }: { util: number }) {
  return (
    <span className="util-badge" style={{ borderColor: utilColor(util), color: utilColor(util) }}>
      {pct(util)}
    </span>
  )
}
