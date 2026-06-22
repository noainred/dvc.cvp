package store

import (
	"sort"
	"testing"

	"github.com/noainred/dvc.cvp/internal/model"
)

func TestIfaceLessNaturalOrder(t *testing.T) {
	// Fixed-config ports: Ethernet2 must sort before Ethernet10.
	assertSorted(t,
		[]string{"Ethernet10", "Ethernet2", "Ethernet1", "Ethernet49", "Management1"},
		[]string{"Ethernet1", "Ethernet2", "Ethernet10", "Ethernet49", "Management1"})

	// Modular chassis ports: Ethernet3/2 must sort before Ethernet3/10.
	assertSorted(t,
		[]string{"Ethernet3/10", "Ethernet4/1", "Ethernet3/2", "Ethernet3/1"},
		[]string{"Ethernet3/1", "Ethernet3/2", "Ethernet3/10", "Ethernet4/1"})
}

func assertSorted(t *testing.T, in, want []string) {
	t.Helper()
	got := append([]string(nil), in...)
	sort.Slice(got, func(i, j int) bool { return ifaceLess(got[i], got[j]) })
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d: got %q want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestRingBounded(t *testing.T) {
	// The ring used by the store must never exceed its capacity.
	r := newRing(3)
	for i := 0; i < 10; i++ {
		r.push(model.Sample{InBps: float64(i)})
	}
	s := r.slice()
	if len(s) != 3 {
		t.Fatalf("len=%d want 3", len(s))
	}
	// Should retain the last three pushes (7,8,9) in order.
	if s[0].InBps != 7 || s[2].InBps != 9 {
		t.Fatalf("window=%v want first=7 last=9", s)
	}
}
