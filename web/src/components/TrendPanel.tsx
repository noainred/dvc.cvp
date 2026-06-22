import type { Sample, Trend, TrendResponse } from '../types'
import { api, usePolling } from '../api'
import { bps } from '../format'
import { TrafficChart } from './TrafficChart'

// TrendPanel shows the long-term (persisted) capacity trend: port-usage growth
// with a linear projection to the threshold, plus total throughput growth and
// a history chart. Series accumulate one sample per minute, so a freshly
// started server shows "데이터 축적 중" until enough points exist.
export function TrendPanel() {
  const { data } = usePolling<TrendResponse>(() => api.trend(30), [], 30000)

  if (!data) return null
  const pts = data.series ?? []
  const chart: Sample[] = pts.map((p) => ({ t: p.t, inBps: p.inBps, outBps: p.outBps }))

  return (
    <section className="panel">
      <h3>용량 추세 · 예측 <span className="cell-sub">(영구저장 · 1분 간격 · 선형 추세)</span></h3>
      {pts.length < 2 ? (
        <p className="hint">추세 데이터 축적 중… 현재 {pts.length}개 샘플 (분당 1개씩 누적, 잠시 후 표시됩니다).</p>
      ) : (
        <>
          <div className="trend-stats">
            <TrendStat
              label="포트 사용률"
              trend={data.portUsage}
              fmt={(v) => `${v.toFixed(1)}%`}
              projectionLabel="80% 도달"
            />
            <TrendStat
              label="총 트래픽 (In+Out)"
              trend={data.throughput}
              fmt={(v) => bps(v)}
              perDay
            />
          </div>
          <TrafficChart samples={chart} height={170} />
        </>
      )}
    </section>
  )
}

function TrendStat({
  label,
  trend,
  fmt,
  projectionLabel,
  perDay
}: {
  label: string
  trend: Trend
  fmt: (v: number) => string
  projectionLabel?: string
  perDay?: boolean
}) {
  const rising = trend.slopePerDay > 0
  return (
    <div className="trend-stat">
      <div className="trend-label">{label}</div>
      <div className="trend-current">{fmt(trend.current)}</div>
      <div className="trend-slope">
        {rising ? '▲' : trend.slopePerDay < 0 ? '▼' : '–'} {perDay ? `${fmt(Math.abs(trend.slopePerDay))}/일` : `${trend.slopePerDay.toFixed(2)}%p/일`}
      </div>
      {projectionLabel &&
        (trend.daysToThreshold && trend.daysToThreshold > 0 ? (
          <div className="trend-proj warn">
            {projectionLabel}까지 약 <b>{Math.round(trend.daysToThreshold)}일</b>
            {trend.reachAt && <> ({new Date(trend.reachAt).toLocaleDateString()})</>}
          </div>
        ) : (
          <div className="trend-proj ok">추세 안정적 (임계 도달 예측 없음)</div>
        ))}
    </div>
  )
}
