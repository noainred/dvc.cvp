// Formatting and color helpers shared across the dashboard.

export function bps(v: number): string {
  if (!v || v < 1) return '0 bps'
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps', 'Pbps']
  let i = 0
  let n = v
  while (n >= 1000 && i < units.length - 1) {
    n /= 1000
    i++
  }
  return `${n.toFixed(n >= 100 ? 0 : 1)} ${units[i]}`
}

export function pct(v: number): string {
  return `${v.toFixed(v >= 10 ? 0 : 1)}%`
}

export function speed(v: number): string {
  if (v >= 1e9) return `${Math.round(v / 1e9)}G`
  if (v >= 1e6) return `${Math.round(v / 1e6)}M`
  return `${v}`
}

export function uptime(sec: number): string {
  if (!sec) return '-'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  return d > 0 ? `${d}d ${h}h` : `${h}h`
}

// statusColor maps a device/data-center health status to a UI color.
export function statusColor(status: string): string {
  switch (status) {
    case 'ok':
      return '#22c55e'
    case 'degraded':
      return '#f59e0b'
    case 'inactive':
      return '#94a3b8'
    case 'unreachable':
      return '#ef4444'
    default:
      return '#64748b'
  }
}

export function statusLabel(status: string): string {
  switch (status) {
    case 'ok':
      return '정상'
    case 'degraded':
      return '저하'
    case 'inactive':
      return '비활성'
    case 'unreachable':
      return '연결불가'
    default:
      return status
  }
}

// utilColor returns a green→amber→red heatmap color for a 0..100 utilization.
export function utilColor(util: number): string {
  const u = Math.max(0, Math.min(100, util))
  const hue = 140 - (140 * u) / 100 // 140=green, 0=red
  return `hsl(${hue}, 65%, 45%)`
}

// portStateColor colors a port square by its state, using the utilization
// heatmap for ports that are in use.
export function portStateColor(state: string, util: number): string {
  switch (state) {
    case 'used':
      return utilColor(util)
    case 'free':
      return '#334155'
    case 'disabled':
      return '#1e293b'
    case 'error':
      return '#ef4444'
    default:
      return '#475569'
  }
}

export function stateLabel(state: string): string {
  switch (state) {
    case 'used':
      return '사용중'
    case 'free':
      return '미사용'
    case 'disabled':
      return '비활성'
    case 'error':
      return '오류'
    default:
      return state
  }
}
