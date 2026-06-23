package history

import (
	"math"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
)

// Trend is a simple linear projection over a time series. It is deliberately a
// least-squares fit (not a forecast model): SlopePerDay is the fitted growth
// rate and DaysToThreshold is the linear extrapolation to Threshold. Callers
// should present it as a trend indicator, not a guarantee.
type Trend struct {
	Points          int        `json:"points"`
	Current         float64    `json:"current"`
	SlopePerDay     float64    `json:"slopePerDay"`
	Threshold       float64    `json:"threshold,omitempty"`
	DaysToThreshold float64    `json:"daysToThreshold,omitempty"` // 0 if not applicable
	ReachAt         *time.Time `json:"reachAt,omitempty"`
}

// PortUsageTrend fits the port-usage percentage and projects when it reaches
// thresholdPct (e.g. 80%).
func PortUsageTrend(points []SummaryPoint, thresholdPct float64) Trend {
	xs, ys := make([]float64, 0, len(points)), make([]float64, 0, len(points))
	if len(points) == 0 {
		return Trend{}
	}
	t0 := points[0].T
	for _, p := range points {
		if p.PortTotal == 0 {
			continue
		}
		xs = append(xs, p.T.Sub(t0).Hours()/24)
		ys = append(ys, float64(p.PortUsed)/float64(p.PortTotal)*100)
	}
	return project(xs, ys, t0, thresholdPct)
}

// ThroughputTrend fits total throughput (in+out bps) growth without a threshold.
func ThroughputTrend(points []SummaryPoint) Trend {
	xs, ys := make([]float64, 0, len(points)), make([]float64, 0, len(points))
	if len(points) == 0 {
		return Trend{}
	}
	t0 := points[0].T
	for _, p := range points {
		xs = append(xs, p.T.Sub(t0).Hours()/24)
		ys = append(ys, p.InBps+p.OutBps)
	}
	return project(xs, ys, t0, 0)
}

// DeviceTrend fits a device's total throughput (in+out bps) growth.
func DeviceTrend(samples []model.Sample) Trend {
	xs, ys := make([]float64, 0, len(samples)), make([]float64, 0, len(samples))
	if len(samples) == 0 {
		return Trend{}
	}
	t0 := samples[0].T
	for _, s := range samples {
		xs = append(xs, s.T.Sub(t0).Hours()/24)
		ys = append(ys, s.InBps+s.OutBps)
	}
	return project(xs, ys, t0, 0)
}

// project runs the least-squares fit and the optional threshold extrapolation.
func project(xs, ys []float64, t0 time.Time, threshold float64) Trend {
	n := len(xs)
	tr := Trend{Points: n, Threshold: threshold}
	if n == 0 {
		return tr
	}
	tr.Current = ys[n-1]
	if n < 2 {
		return tr
	}
	slope, intercept, ok := linreg(xs, ys)
	if !ok {
		return tr
	}
	tr.SlopePerDay = slope
	if threshold > 0 && slope > 0 {
		lastX := xs[n-1]
		// fitted value at lastX, then days until it reaches threshold
		fitted := slope*lastX + intercept
		days := (threshold - fitted) / slope
		if days > 0 && !math.IsInf(days, 0) {
			tr.DaysToThreshold = days
			at := t0.Add(time.Duration((lastX + days) * float64(24*time.Hour)))
			tr.ReachAt = &at
		}
	}
	return tr
}

// linreg returns the slope and intercept of the least-squares line y = a*x + b.
func linreg(xs, ys []float64) (slope, intercept float64, ok bool) {
	n := float64(len(xs))
	var sx, sy, sxx, sxy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, 0, false
	}
	slope = (n*sxy - sx*sy) / den
	intercept = (sy - slope*sx) / n
	return slope, intercept, true
}
