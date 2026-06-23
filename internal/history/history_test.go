package history

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
)

func TestRecordRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "h.db"), time.Hour)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	s.Record(
		model.Summary{PortUsed: 10, PortTotal: 100, TotalInBps: 1e9, TotalOutBps: 2e9},
		[]model.Device{{Serial: "X1", InBps: 5e8, OutBps: 6e8}},
	)

	sum := s.SummarySeries(time.Now().Add(-2 * time.Minute))
	if len(sum) != 1 || sum[0].PortUsed != 10 || sum[0].PortTotal != 100 {
		t.Fatalf("summary series = %+v", sum)
	}
	dev := s.DeviceSeries("X1", time.Now().Add(-2*time.Minute))
	if len(dev) != 1 || dev[0].InBps != 5e8 || dev[0].OutBps != 6e8 {
		t.Fatalf("device series = %+v", dev)
	}
	if other := s.DeviceSeries("nope", time.Now().Add(-2*time.Minute)); len(other) != 0 {
		t.Fatalf("unexpected series for unknown device: %+v", other)
	}
}

func TestNilStoreIsNoOp(t *testing.T) {
	var s *Store
	s.Record(model.Summary{}, nil) // must not panic
	if len(s.SummarySeries(time.Now())) != 0 || len(s.DeviceSeries("x", time.Now())) != 0 {
		t.Fatal("nil store should return empty series")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
}

func TestPortUsageTrendProjection(t *testing.T) {
	base := time.Now().Add(-10 * 24 * time.Hour)
	var pts []SummaryPoint
	for i := 0; i <= 10; i++ {
		pts = append(pts, SummaryPoint{
			T: base.Add(time.Duration(i) * 24 * time.Hour), PortUsed: 50 + i*2, PortTotal: 100,
		})
	}
	// Usage grows 50%→70% at +2%/day; from 70% it takes ~5 days to hit 80%.
	tr := PortUsageTrend(pts, 80)
	if math.Abs(tr.SlopePerDay-2) > 0.05 {
		t.Errorf("slope=%.3f want ~2/day", tr.SlopePerDay)
	}
	if math.Abs(tr.DaysToThreshold-5) > 0.5 {
		t.Errorf("daysToThreshold=%.3f want ~5", tr.DaysToThreshold)
	}
	if tr.ReachAt == nil {
		t.Error("ReachAt should be set for a rising trend")
	}
}

func TestTrendFlatNoProjection(t *testing.T) {
	base := time.Now().Add(-5 * 24 * time.Hour)
	var pts []SummaryPoint
	for i := 0; i <= 5; i++ {
		pts = append(pts, SummaryPoint{T: base.Add(time.Duration(i) * 24 * time.Hour), PortUsed: 40, PortTotal: 100})
	}
	tr := PortUsageTrend(pts, 80)
	if tr.DaysToThreshold != 0 {
		t.Errorf("flat trend should not project a threshold reach, got %.2f days", tr.DaysToThreshold)
	}
}
