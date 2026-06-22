// Package flows produces top-talker (sFlow/IPFIX-style) conversations for the
// dashboard. In this build flows are synthesized from each device's measured
// throughput so the feature is fully usable in demo mode; for production this
// is the integration point for a real sFlow/IPFIX collector or the CloudVision
// flow API (replace Top with a reader over collected flow records).
package flows

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
	"github.com/noainred/dvc.cvp/internal/store"
)

var apps = []struct {
	name  string
	port  int
	proto string
}{
	{"HTTPS", 443, "TCP"}, {"HTTP", 80, "TCP"}, {"MySQL", 3306, "TCP"},
	{"Kafka", 9092, "TCP"}, {"NFS", 2049, "TCP"}, {"gRPC", 50051, "TCP"},
	{"Redis", 6379, "TCP"}, {"S3", 443, "TCP"}, {"DNS", 53, "UDP"}, {"Ceph", 6789, "TCP"},
}

// Top returns the busiest flows across the (optionally filtered) fleet. Results
// are stable within a 30-second bucket and vary over time.
func Top(s *store.Store, dc, device string, limit int) []model.Flow {
	if limit <= 0 {
		limit = 20
	}
	bucket := time.Now().Unix() / 30
	out := []model.Flow{}
	for _, d := range s.Devices(dc) {
		if device != "" && d.Serial != device {
			continue
		}
		total := d.InBps + d.OutBps
		if total < 1 {
			continue
		}
		rng := rand.New(rand.NewSource(int64(hash(d.Serial) ^ uint64(bucket))))
		n := 3 + rng.Intn(4)
		weights := make([]float64, n)
		var sum float64
		for i := range weights {
			weights[i] = rng.Float64() + 0.2
			sum += weights[i]
		}
		for i := 0; i < n; i++ {
			a := apps[rng.Intn(len(apps))]
			out = append(out, model.Flow{
				DataCenter: d.DataCenter, Device: d.Serial, Hostname: d.Hostname,
				SrcIP: ip(rng), DstIP: ip(rng), Proto: a.proto, Port: a.port, App: a.name,
				Bps: total * weights[i] / sum,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bps > out[j].Bps })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func ip(r *rand.Rand) string {
	return fmt.Sprintf("10.%d.%d.%d", r.Intn(254)+1, r.Intn(255), r.Intn(254)+1)
}

func hash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
