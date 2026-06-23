package store

import "github.com/noainred/dvc.cvp/internal/model"

// ring is a fixed-capacity circular buffer of throughput samples. Once full,
// the oldest sample is overwritten so memory stays bounded regardless of how
// long the collector runs.
type ring struct {
	buf   []model.Sample
	size  int
	start int // index of the oldest element
	count int
}

func newRing(capacity int) *ring {
	return &ring{buf: make([]model.Sample, capacity), size: capacity}
}

func (r *ring) push(s model.Sample) {
	idx := (r.start + r.count) % r.size
	if r.count < r.size {
		r.buf[idx] = s
		r.count++
		return
	}
	// full: overwrite oldest and advance the window.
	r.buf[r.start] = s
	r.start = (r.start + 1) % r.size
}

// slice returns the samples in chronological order (oldest first).
func (r *ring) slice() []model.Sample {
	out := make([]model.Sample, r.count)
	for i := 0; i < r.count; i++ {
		out[i] = r.buf[(r.start+i)%r.size]
	}
	return out
}
