// Package lanz produces LANZ-style queue-congestion / microburst events for the
// dashboard. Events are synthesized from per-interface utilization so the
// feature works in demo mode; for production this is the integration point for
// real LANZ streaming (EOS `queue-monitor length`, LANZ gRPC, or CloudVision),
// which would replace Events with a reader over streamed congestion records.
package lanz

import (
	"hash/fnv"
	"math/rand"
	"sort"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
	"github.com/noainred/dvc.cvp/internal/store"
)

// Events returns recent congestion events across the (optionally filtered)
// fleet, deepest queue first. Results are stable within a 30-second bucket.
func Events(s *store.Store, dc, device string, limit int) []model.Congestion {
	if limit <= 0 {
		limit = 50
	}
	bucket := time.Now().Unix() / 30
	out := []model.Congestion{}
	for _, d := range s.Devices(dc) {
		if device != "" && d.Serial != device {
			continue
		}
		for _, ifc := range s.Interfaces(d.Serial) {
			if ifc.State != model.PortUsed || ifc.UtilPct < 55 {
				continue
			}
			rng := rand.New(rand.NewSource(int64(hash(d.Serial+ifc.Name) ^ uint64(bucket))))
			// Microburst likelihood rises with utilization.
			if rng.Float64() > (ifc.UtilPct-50)/55 {
				continue
			}
			depth := int(ifc.UtilPct / 100 * 2200 * (0.5 + rng.Float64()))
			sev := model.SevWarning
			if depth > 1500 {
				sev = model.SevCritical
			}
			out = append(out, model.Congestion{
				DataCenter: d.DataCenter, Device: d.Serial, Hostname: d.Hostname, Interface: ifc.Name,
				QueueDepthKB: depth, DurationMs: 1 + rng.Intn(60), Drops: rng.Intn(depth/200 + 1),
				Severity: sev, Time: time.Now().Add(-time.Duration(rng.Intn(60)) * time.Second),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QueueDepthKB > out[j].QueueDepthKB })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func hash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
