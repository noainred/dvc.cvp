// Package store is the in-memory, concurrency-safe cache of the current fleet
// state plus a bounded per-interface history used to draw throughput charts.
package store

import (
	"sort"
	"sync"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
)

// Store holds the latest snapshot of every data center, device and interface
// along with a ring buffer of throughput samples per interface and per device.
type Store struct {
	mu sync.RWMutex

	historyPoints int

	dcs     map[string]*model.DataCenter
	dcOrder []string

	devices map[string]*model.Device // keyed by serial

	ifaces map[string]map[string]*model.Interface // serial -> name -> iface

	ifaceHist  map[string]map[string]*ring // serial -> name -> samples
	deviceHist map[string]*ring            // serial -> aggregate samples

	updatedAt time.Time
}

// New creates an empty store retaining historyPoints samples per series.
func New(historyPoints int) *Store {
	if historyPoints <= 0 {
		historyPoints = 360
	}
	return &Store{
		historyPoints: historyPoints,
		dcs:           map[string]*model.DataCenter{},
		devices:       map[string]*model.Device{},
		ifaces:        map[string]map[string]*model.Interface{},
		ifaceHist:     map[string]map[string]*ring{},
		deviceHist:    map[string]*ring{},
	}
}

// SetDataCenters seeds the list of known sites (preserving order).
func (s *Store) SetDataCenters(dcs []model.DataCenter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dcOrder = s.dcOrder[:0]
	for i := range dcs {
		dc := dcs[i]
		if _, ok := s.dcs[dc.ID]; !ok {
			s.dcs[dc.ID] = &dc
		}
		s.dcOrder = append(s.dcOrder, dc.ID)
	}
}

// UpdateDataCenter replaces the cached health/rollup for a single site.
func (s *Store) UpdateDataCenter(dc model.DataCenter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := dc
	s.dcs[dc.ID] = &cp
	if !contains(s.dcOrder, dc.ID) {
		s.dcOrder = append(s.dcOrder, dc.ID)
	}
}

// ReplaceDevice upserts a device record.
func (s *Store) ReplaceDevice(d model.Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := d
	s.devices[d.Serial] = &cp
}

// ReplaceInterfaces upserts all interfaces for a device and appends a history
// sample for each interface plus an aggregate sample for the device.
func (s *Store) ReplaceInterfaces(serial string, ifaces []model.Interface, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m := s.ifaces[serial]
	if m == nil {
		m = map[string]*model.Interface{}
		s.ifaces[serial] = m
	}
	hm := s.ifaceHist[serial]
	if hm == nil {
		hm = map[string]*ring{}
		s.ifaceHist[serial] = hm
	}

	var devIn, devOut float64
	for i := range ifaces {
		ifc := ifaces[i]
		cp := ifc
		m[ifc.Name] = &cp
		r := hm[ifc.Name]
		if r == nil {
			r = newRing(s.historyPoints)
			hm[ifc.Name] = r
		}
		r.push(model.Sample{T: t, InBps: ifc.InBps, OutBps: ifc.OutBps})
		devIn += ifc.InBps
		devOut += ifc.OutBps
	}

	dr := s.deviceHist[serial]
	if dr == nil {
		dr = newRing(s.historyPoints)
		s.deviceHist[serial] = dr
	}
	dr.push(model.Sample{T: t, InBps: devIn, OutBps: devOut})

	s.updatedAt = t
}

// DataCenters returns a copy of all sites in their configured order.
func (s *Store) DataCenters() []model.DataCenter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.DataCenter, 0, len(s.dcOrder))
	for _, id := range s.dcOrder {
		if dc := s.dcs[id]; dc != nil {
			out = append(out, *dc)
		}
	}
	return out
}

// Devices returns all devices, optionally filtered by data center id.
func (s *Store) Devices(dcID string) []model.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Device, 0, len(s.devices))
	for _, d := range s.devices {
		if dcID == "" || d.DataCenter == dcID {
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DataCenter != out[j].DataCenter {
			return out[i].DataCenter < out[j].DataCenter
		}
		return out[i].Hostname < out[j].Hostname
	})
	return out
}

// Device returns a single device by serial.
func (s *Store) Device(serial string) (model.Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if d, ok := s.devices[serial]; ok {
		return *d, true
	}
	return model.Device{}, false
}

// Interfaces returns the interfaces of a device sorted by natural port order.
func (s *Store) Interfaces(serial string) []model.Interface {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.ifaces[serial]
	out := make([]model.Interface, 0, len(m))
	for _, ifc := range m {
		out = append(out, *ifc)
	}
	sort.Slice(out, func(i, j int) bool { return ifaceLess(out[i].Name, out[j].Name) })
	return out
}

// InterfaceHistory returns the throughput samples for one interface.
func (s *Store) InterfaceHistory(serial, name string) []model.Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if hm := s.ifaceHist[serial]; hm != nil {
		if r := hm[name]; r != nil {
			return r.slice()
		}
	}
	return nil
}

// DeviceHistory returns the aggregate throughput samples for a device.
func (s *Store) DeviceHistory(serial string) []model.Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r := s.deviceHist[serial]; r != nil {
		return r.slice()
	}
	return nil
}

// Summary computes the global rollup across the whole fleet.
func (s *Store) Summary() model.Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sum := model.Summary{UpdatedAt: s.updatedAt}
	fam := map[string]int{}
	for _, id := range s.dcOrder {
		dc := s.dcs[id]
		if dc == nil {
			continue
		}
		sum.DataCenters++
		if dc.Status == model.StatusOK {
			sum.DataCentersOK++
		}
	}
	for _, d := range s.devices {
		sum.Devices++
		if d.Status == model.StatusOK {
			sum.DevicesUp++
		}
		sum.TotalInBps += d.InBps
		sum.TotalOutBps += d.OutBps
		if d.Family != "" {
			fam[d.Family]++
		}
	}
	for _, ifaces := range s.ifaces {
		for _, ifc := range ifaces {
			sum.PortTotal++
			switch ifc.State {
			case model.PortUsed:
				sum.PortUsed++
			case model.PortFree:
				sum.PortFree++
				if ifc.HasTransceiver {
					sum.PortFreeReady++
				} else {
					sum.PortFreeEmpty++
				}
			case model.PortDisabled:
				sum.PortDisabled++
			case model.PortError:
				sum.PortError++
			}
			if ifc.OpticAlarm != "" {
				sum.OpticAlarms++
			}
		}
	}
	fams := make([]model.FamilyCount, 0, len(fam))
	for f, c := range fam {
		fams = append(fams, model.FamilyCount{Family: f, Count: c})
	}
	sort.Slice(fams, func(i, j int) bool { return fams[i].Family < fams[j].Family })
	sum.ByFamily = fams
	return sum
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
