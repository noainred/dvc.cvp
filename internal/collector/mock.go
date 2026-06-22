package collector

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
)

// Mock is a self-contained fleet simulator used by the demo mode. It builds a
// realistic multi-data-center Arista topology (7280 leaves, 7504 spines, 7010
// management switches) and produces continuously increasing interface counters
// so the collector exercises exactly the same rate-computation path it would
// with live CloudVision data.
type Mock struct {
	dcs     []model.DataCenter
	devices []*mockDevice
	last    time.Time
	rnd     *rand.Rand
}

type mockDevice struct {
	dev   model.Device
	ports []*mockPort
}

type mockPort struct {
	name       string
	desc       string
	admin      string
	oper       string
	speed      int64
	neighbor   string
	base       float64 // baseline utilization fraction 0..1
	amp        float64 // sinusoidal amplitude
	phase      float64
	asym       float64 // in/out asymmetry
	errProne   bool
	inOct      uint64
	outOct     uint64
	inErr      uint64
	outErr     uint64
	inDisc     uint64
	outDisc    uint64
	lastChange time.Time
}

// site describes one simulated data center for the demo topology.
type site struct {
	id, name, region, city, proxy, status string
	leaves                                int
	spines                                int
}

// demoSites models a Korea-HQ operator with 20+ global data centers reached
// over per-site SSH jump hosts.
var demoSites = []site{
	{"kr-seoul-hq", "Seoul HQ", "KR", "Seoul", "ssh", model.StatusOK, 6, 2},
	{"kr-icn-gpa", "Incheon Gangpaedo", "KR", "Incheon", "ssh", model.StatusOK, 6, 2},
	{"kr-busan", "Busan East", "KR", "Busan", "ssh", model.StatusOK, 4, 2},
	{"kr-pangyo", "Pangyo Tech", "KR", "Seongnam", "ssh", model.StatusOK, 4, 2},
	{"kr-daejeon", "Daejeon Research", "KR", "Daejeon", "ssh", model.StatusDegraded, 3, 1},
	{"jp-tokyo", "Tokyo KIX", "APAC", "Tokyo", "ssh", model.StatusOK, 4, 2},
	{"jp-osaka", "Osaka", "APAC", "Osaka", "ssh", model.StatusOK, 3, 1},
	{"sg-singapore", "Singapore SIN1", "APAC", "Singapore", "ssh", model.StatusOK, 4, 2},
	{"hk-hongkong", "Hong Kong", "APAC", "Hong Kong", "ssh", model.StatusOK, 3, 1},
	{"us-ashburn", "Ashburn US-East", "AMER", "Ashburn", "ssh", model.StatusOK, 5, 2},
	{"us-sjc", "San Jose US-West", "AMER", "San Jose", "ssh", model.StatusOK, 4, 2},
	{"de-frankfurt", "Frankfurt EU", "EMEA", "Frankfurt", "ssh", model.StatusOK, 4, 2},
	{"nl-amsterdam", "Amsterdam EU", "EMEA", "Amsterdam", "ssh", model.StatusOK, 3, 1},
	{"gb-london", "London UK", "EMEA", "London", "ssh", model.StatusOK, 3, 1},
	{"au-sydney", "Sydney", "APAC", "Sydney", "ssh", model.StatusOK, 3, 1},
	{"in-mumbai", "Mumbai", "APAC", "Mumbai", "ssh", model.StatusOK, 3, 1},
	{"br-saopaulo", "Sao Paulo", "AMER", "Sao Paulo", "ssh", model.StatusOK, 3, 1},
	{"ae-dubai", "Dubai", "EMEA", "Dubai", "ssh", model.StatusOK, 3, 1},
	{"kr-gwangju", "Gwangju Edge", "KR", "Gwangju", "ssh", model.StatusUnreachable, 2, 1},
	{"vn-hanoi", "Hanoi Edge", "APAC", "Hanoi", "ssh", model.StatusOK, 2, 1},
	{"id-jakarta", "Jakarta Edge", "APAC", "Jakarta", "ssh", model.StatusOK, 2, 1},
}

// NewMock builds the demo topology.
func NewMock() *Mock {
	m := &Mock{rnd: rand.New(rand.NewSource(42))}
	for _, s := range demoSites {
		m.dcs = append(m.dcs, model.DataCenter{
			ID: s.id, Name: s.name, Region: s.region, City: s.city,
			ProxyType: s.proxy, Status: s.status,
		})
		if s.status == model.StatusUnreachable {
			continue // simulate a site we currently cannot reach
		}
		m.buildSite(s)
	}
	return m
}

func (m *Mock) buildSite(s site) {
	// Spines: 7504 modular chassis.
	for i := 1; i <= s.spines; i++ {
		host := fmt.Sprintf("%s-spine%d", s.id, i)
		d := &mockDevice{dev: model.Device{
			Serial: serial("SP", s.id, i), Hostname: host,
			Model: "DCS-7504N", Family: model.Family7504, Role: "spine",
			Version: "EOS-4.31.2F", MgmtIP: fmt.Sprintf("10.%d.%d.%d", siteOctet(s.id), 0, i),
			DataCenter: s.id, Status: deviceStatus(s.status, m.rnd),
			UptimeSec: int64(3600 * (240 + m.rnd.Intn(2000))),
		}}
		// 4 line cards x 12 x 100G fabric ports toward leaves/cores.
		for card := 3; card <= 6; card++ {
			for p := 1; p <= 12; p++ {
				name := fmt.Sprintf("Ethernet%d/%d", card, p)
				connected := m.rnd.Float64() < 0.85
				d.ports = append(d.ports, m.newPort(name, 100e9, connected, 0.55, true,
					fmt.Sprintf("%s-leaf%d", s.id, ((card-3)*12+p)%maxInt(s.leaves, 1)+1)))
			}
		}
		d.ports = append(d.ports, m.mgmtPort())
		m.devices = append(m.devices, d)
	}

	// Leaves: 7280 with 48x25G host ports + 8x100G uplinks.
	for i := 1; i <= s.leaves; i++ {
		host := fmt.Sprintf("%s-leaf%d", s.id, i)
		d := &mockDevice{dev: model.Device{
			Serial: serial("LF", s.id, i), Hostname: host,
			Model: "DCS-7280SR3-48YC8", Family: model.Family7280, Role: "leaf",
			Version: "EOS-4.31.2F", MgmtIP: fmt.Sprintf("10.%d.%d.%d", siteOctet(s.id), 1, i),
			DataCenter: s.id, Status: deviceStatus(s.status, m.rnd),
			UptimeSec: int64(3600 * (100 + m.rnd.Intn(2000))),
		}}
		for p := 1; p <= 48; p++ {
			name := fmt.Sprintf("Ethernet%d", p)
			r := m.rnd.Float64()
			switch {
			case r < 0.60: // server-facing, in use
				d.ports = append(d.ports, m.newPort(name, 25e9, true, 0.30, false,
					fmt.Sprintf("srv-%s-%02d", s.id, p)))
			case r < 0.90: // patched but unused / available
				port := m.newPort(name, 25e9, false, 0, false, "")
				d.ports = append(d.ports, port)
			default: // administratively disabled
				port := m.newPort(name, 25e9, false, 0, false, "")
				port.admin = "down"
				port.oper = "disabled"
				d.ports = append(d.ports, port)
			}
		}
		// uplinks to spines, busy and a couple error-prone.
		for p := 49; p <= 56; p++ {
			name := fmt.Sprintf("Ethernet%d", p)
			port := m.newPort(name, 100e9, true, 0.50, true,
				fmt.Sprintf("%s-spine%d", s.id, (p-49)%maxInt(s.spines, 1)+1))
			if p == 56 && m.rnd.Float64() < 0.5 {
				port.errProne = true
			}
			d.ports = append(d.ports, port)
		}
		d.ports = append(d.ports, m.mgmtPort())
		m.devices = append(m.devices, d)
	}

	// One 7010 management/OOB switch per site.
	host := fmt.Sprintf("%s-mgmt1", s.id)
	d := &mockDevice{dev: model.Device{
		Serial: serial("MG", s.id, 1), Hostname: host,
		Model: "DCS-7010T-48", Family: model.Family7010, Role: "mgmt",
		Version: "EOS-4.30.5M", MgmtIP: fmt.Sprintf("10.%d.250.1", siteOctet(s.id)),
		DataCenter: s.id, Status: deviceStatus(s.status, m.rnd),
		UptimeSec: int64(3600 * (500 + m.rnd.Intn(3000))),
	}}
	for p := 1; p <= 48; p++ {
		name := fmt.Sprintf("Ethernet%d", p)
		connected := m.rnd.Float64() < 0.5
		d.ports = append(d.ports, m.newPort(name, 1e9, connected, 0.05, false, ""))
	}
	for p := 49; p <= 52; p++ {
		name := fmt.Sprintf("Ethernet%d", p)
		d.ports = append(d.ports, m.newPort(name, 10e9, true, 0.15, false, "core"))
	}
	d.ports = append(d.ports, m.mgmtPort())
	m.devices = append(m.devices, d)
}

func (m *Mock) newPort(name string, speed int64, connected bool, base float64, uplink bool, neighbor string) *mockPort {
	p := &mockPort{
		name:       name,
		speed:      speed,
		admin:      "up",
		oper:       "notconnect",
		base:       base,
		amp:        base * (0.4 + 0.4*m.rnd.Float64()),
		phase:      m.rnd.Float64() * 2 * math.Pi,
		asym:       0.6 + 0.6*m.rnd.Float64(),
		neighbor:   neighbor,
		lastChange: time.Now().Add(-time.Duration(m.rnd.Intn(72)) * time.Hour),
	}
	if connected {
		p.oper = "connected"
		if uplink {
			p.desc = "uplink"
		} else if neighbor != "" {
			p.desc = "to " + neighbor
		}
	}
	return p
}

func (m *Mock) mgmtPort() *mockPort {
	return &mockPort{
		name: "Management1", speed: 1e9, admin: "up", oper: "connected",
		base: 0.01, amp: 0.005, asym: 1, neighbor: "oob-mgmt",
		lastChange: time.Now().Add(-720 * time.Hour),
	}
}

// Poll advances every interface counter by the traffic generated during the
// elapsed interval and returns the snapshot.
func (m *Mock) Poll(_ context.Context) (*Snapshot, error) {
	now := time.Now()
	dt := 0.0
	if !m.last.IsZero() {
		dt = now.Sub(m.last).Seconds()
	}
	m.last = now
	elapsed := now.Unix()

	devs := make([]DeviceSample, 0, len(m.devices))
	for _, md := range m.devices {
		ports := make([]PortSample, 0, len(md.ports))
		for _, p := range md.ports {
			if p.oper == "connected" || p.oper == "up" {
				// daily-ish wave plus jitter, clamped into [0,1].
				wave := p.base + p.amp*math.Sin(2*math.Pi*float64(elapsed)/600+p.phase)
				wave += (m.rnd.Float64() - 0.5) * 0.05
				if wave < 0 {
					wave = 0
				}
				if wave > 0.98 {
					wave = 0.98
				}
				inBps := wave * float64(p.speed) * (p.asym)
				outBps := wave * float64(p.speed) / (p.asym)
				inBps = math.Min(inBps, float64(p.speed))
				outBps = math.Min(outBps, float64(p.speed))
				p.inOct += uint64(inBps / 8 * dt)
				p.outOct += uint64(outBps / 8 * dt)
				if p.errProne && m.rnd.Float64() < 0.3 {
					p.inErr += uint64(m.rnd.Intn(50))
					p.inDisc += uint64(m.rnd.Intn(20))
				}
			}
			ports = append(ports, PortSample{
				Name: p.name, Description: p.desc, AdminStatus: p.admin,
				OperStatus: p.oper, SpeedBps: p.speed, Neighbor: p.neighbor,
				LastChange: p.lastChange,
				Counters: model.Counters{
					InOctets: p.inOct, OutOctets: p.outOct,
					InErrors: p.inErr, OutErrors: p.outErr,
					InDiscards: p.inDisc, OutDiscards: p.outDisc,
					Timestamp: now,
				},
			})
		}
		devs = append(devs, DeviceSample{Device: md.dev, Ports: ports})
	}

	// Return a fresh copy of the data center metadata each poll.
	dcs := make([]model.DataCenter, len(m.dcs))
	copy(dcs, m.dcs)
	return &Snapshot{DataCenters: dcs, Devices: devs}, nil
}

func (m *Mock) Name() string { return "demo" }
func (m *Mock) Close() error { return nil }

func deviceStatus(siteStatus string, rnd *rand.Rand) string {
	if siteStatus == model.StatusUnreachable {
		return model.StatusUnreachable
	}
	if rnd.Float64() < 0.06 {
		return model.StatusInactive
	}
	return model.StatusOK
}

func serial(prefix, site string, i int) string {
	return fmt.Sprintf("%s%s%03d", prefix, siteCode(site), i)
}

func siteCode(site string) string {
	code := ""
	for _, r := range site {
		if r >= 'a' && r <= 'z' {
			code += string(r - 32)
		}
		if len(code) >= 4 {
			break
		}
	}
	return code
}

func siteOctet(site string) int {
	h := 0
	for _, r := range site {
		h = (h*31 + int(r)) % 254
	}
	return h + 1
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
