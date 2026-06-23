import type { Sample } from '../types'
import { bps } from '../format'

// TrafficChart draws an in/out throughput area chart from a series of samples
// using plain SVG (no charting dependency).
export function TrafficChart({ samples, height = 200 }: { samples: Sample[]; height?: number }) {
  const W = 760
  const H = height
  const pad = { l: 70, r: 14, t: 14, b: 24 }

  if (!samples || samples.length < 2) {
    return <div className="chart-empty">데이터 수집 중… (다음 폴링 주기를 기다리세요)</div>
  }

  const innerW = W - pad.l - pad.r
  const innerH = H - pad.t - pad.b
  const maxV = Math.max(1, ...samples.map((s) => Math.max(s.inBps, s.outBps)))
  const n = samples.length

  const x = (i: number) => pad.l + (i / (n - 1)) * innerW
  const y = (v: number) => pad.t + innerH - (v / maxV) * innerH

  const line = (key: 'inBps' | 'outBps') =>
    samples.map((s, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(s[key]).toFixed(1)}`).join(' ')
  const area = (key: 'inBps' | 'outBps') =>
    `${line(key)} L${x(n - 1).toFixed(1)},${y(0).toFixed(1)} L${x(0).toFixed(1)},${y(0).toFixed(1)} Z`

  const grid = [0, 0.25, 0.5, 0.75, 1]
  const last = samples[n - 1]
  const t0 = new Date(samples[0].t)
  const t1 = new Date(last.t)

  return (
    <div className="traffic-chart">
      <svg viewBox={`0 0 ${W} ${H}`} width="100%" role="img" aria-label="throughput chart">
        {grid.map((g) => {
          const yy = pad.t + innerH - g * innerH
          return (
            <g key={g}>
              <line x1={pad.l} y1={yy} x2={W - pad.r} y2={yy} className="grid-line" />
              <text x={pad.l - 8} y={yy + 4} className="axis-label" textAnchor="end">
                {bps(maxV * g)}
              </text>
            </g>
          )
        })}
        <path d={area('inBps')} className="area-in" />
        <path d={area('outBps')} className="area-out" />
        <path d={line('inBps')} className="stroke-in" />
        <path d={line('outBps')} className="stroke-out" />
        <text x={pad.l} y={H - 6} className="axis-label" textAnchor="start">
          {t0.toLocaleTimeString()}
        </text>
        <text x={W - pad.r} y={H - 6} className="axis-label" textAnchor="end">
          {t1.toLocaleTimeString()}
        </text>
      </svg>
      <div className="chart-legend">
        <span className="legend-item">
          <span className="swatch in" /> 인입 {bps(last.inBps)}
        </span>
        <span className="legend-item">
          <span className="swatch out" /> 인출 {bps(last.outBps)}
        </span>
      </div>
    </div>
  )
}
