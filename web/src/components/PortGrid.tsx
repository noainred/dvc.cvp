import type { Interface } from '../types'
import { bps, dbm, opticAlarmLabel, portStateColor, pct, speed, stateLabel } from '../format'

interface Props {
  interfaces: Interface[]
  selected: string | null
  onSelect: (name: string) => void
}

// PortGrid renders every interface as a heatmap square: in-use ports are
// colored by utilization (green→red); free ports are split into "optic ready"
// (teal — a transceiver is installed) and "empty" (slate). A port with an
// optical (DOM) alarm gets a red ring. This is the "which port is used vs.
// unused, and which unused ports already have an optic" view.
export function PortGrid({ interfaces, selected, onSelect }: Props) {
  const c = { used: 0, freeReady: 0, freeEmpty: 0, disabled: 0, error: 0, alarm: 0 }
  for (const i of interfaces) {
    if (i.opticAlarm) c.alarm++
    switch (i.state) {
      case 'used':
        c.used++
        break
      case 'free':
        i.hasTransceiver ? c.freeReady++ : c.freeEmpty++
        break
      case 'disabled':
        c.disabled++
        break
      case 'error':
        c.error++
        break
    }
  }

  return (
    <div>
      <div className="port-legend">
        <LegendChip color={portStateColor('used', 30)} label={`사용중 ${c.used}`} />
        <LegendChip color={portStateColor('free', 0, true)} label={`미사용·모듈O ${c.freeReady}`} />
        <LegendChip color={portStateColor('free', 0, false)} label={`미사용·모듈X ${c.freeEmpty}`} />
        <LegendChip color={portStateColor('disabled', 0)} label={`비활성 ${c.disabled}`} />
        <LegendChip color={portStateColor('error', 0)} label={`오류 ${c.error}`} />
        {c.alarm > 0 && <LegendChip color="#ef4444" label={`광경보 ${c.alarm}`} ring />}
        <span className="port-legend-scale">
          사용률 <span className="scale-bar" /> 0→100%
        </span>
      </div>
      <div className="port-grid">
        {interfaces.map((i) => (
          <button
            key={i.name}
            className={`port-cell${selected === i.name ? ' selected' : ''}${i.opticAlarm ? ' alarm' : ''}`}
            style={{ background: portStateColor(i.state, i.utilPct, i.hasTransceiver) }}
            onClick={() => onSelect(i.name)}
            title={tooltip(i)}
          >
            <span className="port-num">{portShort(i.name)}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

function tooltip(i: Interface): string {
  const lines = [
    i.name + (i.description ? ` (${i.description})` : ''),
    `상태: ${stateLabel(i.state)} / ${i.adminStatus}-${i.operStatus}`,
    `속도: ${speed(i.speedBps)}  사용률: ${pct(i.utilPct)}`,
    `인입: ${bps(i.inBps)}  인출: ${bps(i.outBps)}`
  ]
  if (i.mediaType || i.hasTransceiver) {
    lines.push(
      `모듈: ${i.hasTransceiver ? '장착' : '없음'}${i.mediaType ? ` (${i.mediaType})` : ''}` +
        (i.xcvrPart ? ` ${i.xcvrPart}` : '')
    )
  }
  if (i.domValid) {
    lines.push(`광: Rx ${dbm(i.rxPowerDbm)} / Tx ${dbm(i.txPowerDbm)}`)
  }
  if (i.opticAlarm) lines.push(`⚠ 광경보: ${opticAlarmLabel(i.opticAlarm)}`)
  if (i.neighbor) lines.push(`이웃: ${i.neighbor}`)
  return lines.join('\n')
}

function LegendChip({ color, label, ring }: { color: string; label: string; ring?: boolean }) {
  return (
    <span className="legend-chip">
      <span className={`chip-swatch${ring ? ' ring' : ''}`} style={{ background: color }} /> {label}
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
