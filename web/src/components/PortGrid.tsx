import type { Interface } from '../types'
import { bps, portStateColor, pct, speed, stateLabel } from '../format'

interface Props {
  interfaces: Interface[]
  selected: string | null
  onSelect: (name: string) => void
}

// PortGrid renders every interface as a heatmap square: in-use ports are
// colored by utilization (green→red), free/disabled/error ports get a fixed
// color. This is the "which port is used vs. unused" view.
export function PortGrid({ interfaces, selected, onSelect }: Props) {
  const counts = { used: 0, free: 0, disabled: 0, error: 0 } as Record<string, number>
  for (const i of interfaces) counts[i.state] = (counts[i.state] ?? 0) + 1

  return (
    <div>
      <div className="port-legend">
        <LegendChip color={portStateColor('used', 30)} label={`사용중 ${counts.used}`} />
        <LegendChip color={portStateColor('free', 0)} label={`미사용 ${counts.free}`} />
        <LegendChip color={portStateColor('disabled', 0)} label={`비활성 ${counts.disabled}`} />
        <LegendChip color={portStateColor('error', 0)} label={`오류 ${counts.error}`} />
        <span className="port-legend-scale">
          사용률 <span className="scale-bar" /> 0→100%
        </span>
      </div>
      <div className="port-grid">
        {interfaces.map((i) => (
          <button
            key={i.name}
            className={`port-cell${selected === i.name ? ' selected' : ''}`}
            style={{ background: portStateColor(i.state, i.utilPct) }}
            onClick={() => onSelect(i.name)}
            title={
              `${i.name}` +
              (i.description ? ` (${i.description})` : '') +
              `\n상태: ${stateLabel(i.state)} / ${i.adminStatus}-${i.operStatus}` +
              `\n속도: ${speed(i.speedBps)} 사용률: ${pct(i.utilPct)}` +
              `\n인입: ${bps(i.inBps)} 인출: ${bps(i.outBps)}` +
              (i.neighbor ? `\n이웃: ${i.neighbor}` : '')
            }
          >
            <span className="port-num">{portShort(i.name)}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

function LegendChip({ color, label }: { color: string; label: string }) {
  return (
    <span className="legend-chip">
      <span className="chip-swatch" style={{ background: color }} /> {label}
    </span>
  )
}

// portShort turns "Ethernet3/1" into "3/1" and "Management1" into "M1" for the
// compact grid cell label.
function portShort(name: string): string {
  if (name.startsWith('Ethernet')) return name.slice('Ethernet'.length)
  if (name.startsWith('Management')) return 'M' + name.slice('Management'.length)
  if (name.startsWith('Port-Channel')) return 'Po' + name.slice('Port-Channel'.length)
  return name
}
