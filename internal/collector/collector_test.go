package collector

import (
	"testing"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
)

func TestRate(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := t0.Add(10 * time.Second)

	// First sample (no previous) -> zero.
	if in, out := rate(model.Counters{}, model.Counters{InOctets: 100, OutOctets: 200, Timestamp: t1}); in != 0 || out != 0 {
		t.Fatalf("first sample: want 0,0 got %v,%v", in, out)
	}

	prev := model.Counters{InOctets: 1000, OutOctets: 2000, Timestamp: t0}
	// 1250 octets in 10s = 1000 bits/s in; 2500 octets = 2000 bits/s out.
	cur := model.Counters{InOctets: 1000 + 1250, OutOctets: 2000 + 2500, Timestamp: t1}
	in, out := rate(prev, cur)
	if in != 1000 || out != 2000 {
		t.Fatalf("rate: want 1000,2000 got %v,%v", in, out)
	}

	// Counter reset (current < previous) -> zero, not negative.
	if in, _ := rate(prev, model.Counters{InOctets: 5, OutOctets: 9999, Timestamp: t1}); in != 0 {
		t.Fatalf("counter reset: want 0 got %v", in)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		admin, oper, want string
	}{
		{"up", "connected", model.PortUsed},
		{"up", "notconnect", model.PortFree},
		{"down", "disabled", model.PortDisabled},
		{"up", "errdisabled", model.PortError},
	}
	for _, c := range cases {
		got := classify(PortSample{AdminStatus: c.admin, OperStatus: c.oper})
		if got != c.want {
			t.Errorf("classify(%s/%s)=%s want %s", c.admin, c.oper, got, c.want)
		}
	}
}

func TestClampPct(t *testing.T) {
	if v := clampPct(-5); v != 0 {
		t.Errorf("clampPct(-5)=%v want 0", v)
	}
	if v := clampPct(150); v != 100 {
		t.Errorf("clampPct(150)=%v want 100", v)
	}
	if v := clampPct(42.5); v != 42.5 {
		t.Errorf("clampPct(42.5)=%v want 42.5", v)
	}
}
