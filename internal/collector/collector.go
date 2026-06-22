// Package collector periodically polls a Provider (live CloudVision or the
// demo generator), turns raw interface counters into throughput/utilization,
// classifies every port as used/free/disabled/error and writes the result into
// the store. After every cycle it invokes an optional notify callback so the
// API layer can push live updates to the browser over SSE.
package collector

import (
	"context"
	"log"
	"time"

	"github.com/noainred/dvc.cvp/internal/model"
	"github.com/noainred/dvc.cvp/internal/store"
)

// PortSample is the raw, provider-supplied state of one interface.
type PortSample struct {
	Name        string
	Description string
	AdminStatus string // "up" | "down"
	OperStatus  string // "connected" | "notconnect" | "disabled" | "errdisabled" | "down"
	SpeedBps    int64
	Neighbor    string
	LastChange  time.Time
	Counters    model.Counters

	// Transceiver / optics (GBIC) inventory and DOM readings.
	HasTransceiver bool
	MediaType      string
	XcvrVendor     string
	XcvrPart       string
	XcvrSerial     string
	DomValid       bool
	TxPowerDbm     float64
	RxPowerDbm     float64
	TempC          float64
	VoltageV       float64
}

// Optical alarm thresholds used to flag a degrading optic.
const (
	rxLowDbm  = -16.0
	txLowDbm  = -11.0
	tempHighC = 70.0
)

// DeviceSample carries a device's metadata plus its port samples for one poll.
type DeviceSample struct {
	Device model.Device
	Ports  []PortSample
}

// Snapshot is one full poll across the whole fleet.
type Snapshot struct {
	DataCenters []model.DataCenter
	Devices     []DeviceSample
}

// Provider is any source of fleet telemetry.
type Provider interface {
	Name() string
	Poll(ctx context.Context) (*Snapshot, error)
	Close() error
}

// Collector drives the polling loop.
type Collector struct {
	provider Provider
	store    *store.Store
	interval time.Duration
	timeout  time.Duration
	notify   func()

	prev map[string]map[string]model.Counters // serial -> name -> last counters
}

// New creates a collector.
func New(p Provider, s *store.Store, interval, timeout time.Duration) *Collector {
	return &Collector{
		provider: p,
		store:    s,
		interval: interval,
		timeout:  timeout,
		prev:     map[string]map[string]model.Counters{},
	}
}

// OnUpdate registers a callback invoked after each successful poll cycle.
func (c *Collector) OnUpdate(fn func()) { c.notify = fn }

// Run polls immediately, then on the configured interval until ctx is done.
func (c *Collector) Run(ctx context.Context) {
	c.pollOnce(ctx)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			c.provider.Close()
			return
		case <-t.C:
			c.pollOnce(ctx)
		}
	}
}

func (c *Collector) pollOnce(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	snap, err := c.provider.Poll(pctx)
	if err != nil {
		log.Printf("collector: poll failed: %v", err)
		return
	}
	now := time.Now()

	// Roll up device stats into their data centers.
	dcAgg := map[string]*model.DataCenter{}
	for i := range snap.DataCenters {
		dc := snap.DataCenters[i]
		dc.LastPoll = now
		cp := dc
		dcAgg[dc.ID] = &cp
	}

	for _, ds := range snap.Devices {
		ifaces := c.deriveInterfaces(ds, now)

		dev := ds.Device
		dev.LastPoll = now
		dev.PortTotal = len(ifaces)
		dev.PortUp, dev.PortUsed, dev.PortFree, dev.PortFreeReady, dev.PortErr = 0, 0, 0, 0, 0
		dev.InBps, dev.OutBps, dev.MaxUtilPct, dev.OpticAlarms = 0, 0, 0, 0
		for _, ifc := range ifaces {
			if ifc.OperStatus == "connected" || ifc.OperStatus == "up" {
				dev.PortUp++
			}
			switch ifc.State {
			case model.PortUsed:
				dev.PortUsed++
			case model.PortFree:
				dev.PortFree++
				if ifc.HasTransceiver {
					dev.PortFreeReady++
				}
			case model.PortError:
				dev.PortErr++
			}
			if ifc.OpticAlarm != "" {
				dev.OpticAlarms++
			}
			dev.InBps += ifc.InBps
			dev.OutBps += ifc.OutBps
			if ifc.UtilPct > dev.MaxUtilPct {
				dev.MaxUtilPct = ifc.UtilPct
			}
		}

		c.store.ReplaceDevice(dev)
		c.store.ReplaceInterfaces(dev.Serial, ifaces, now)

		if dc := dcAgg[dev.DataCenter]; dc != nil {
			dc.DeviceCount++
			if dev.Status == model.StatusOK {
				dc.DeviceUp++
			}
			dc.PortTotal += dev.PortTotal
			dc.PortUsed += dev.PortUsed
			dc.PortFree += dev.PortFree
			dc.PortFreeReady += dev.PortFreeReady
			dc.OpticAlarms += dev.OpticAlarms
			dc.InBps += dev.InBps
			dc.OutBps += dev.OutBps
			if dev.MaxUtilPct > dc.MaxUtilPct {
				dc.MaxUtilPct = dev.MaxUtilPct
			}
		}
	}

	for _, dc := range dcAgg {
		c.store.UpdateDataCenter(*dc)
	}

	if c.notify != nil {
		c.notify()
	}
}

// deriveInterfaces converts raw port samples into model.Interface records,
// computing per-interface throughput from the delta against the previous poll
// and classifying each port's state.
func (c *Collector) deriveInterfaces(ds DeviceSample, now time.Time) []model.Interface {
	prev := c.prev[ds.Device.Serial]
	if prev == nil {
		prev = map[string]model.Counters{}
		c.prev[ds.Device.Serial] = prev
	}

	out := make([]model.Interface, 0, len(ds.Ports))
	for _, p := range ds.Ports {
		inBps, outBps := rate(prev[p.Name], p.Counters)
		prev[p.Name] = p.Counters

		ifc := model.Interface{
			Device:      ds.Device.Serial,
			Name:        p.Name,
			Description: p.Description,
			AdminStatus: p.AdminStatus,
			OperStatus:  p.OperStatus,
			SpeedBps:    p.SpeedBps,
			InBps:       inBps,
			OutBps:      outBps,
			InErrors:    int64(p.Counters.InErrors),
			OutErrors:   int64(p.Counters.OutErrors),
			InDiscards:  int64(p.Counters.InDiscards),
			OutDiscards: int64(p.Counters.OutDiscards),
			LastChange:  p.LastChange,
			Neighbor:    p.Neighbor,
		}
		if p.SpeedBps > 0 {
			ifc.InUtilPct = clampPct(inBps / float64(p.SpeedBps) * 100)
			ifc.OutUtilPct = clampPct(outBps / float64(p.SpeedBps) * 100)
		}
		ifc.UtilPct = max(ifc.InUtilPct, ifc.OutUtilPct)
		ifc.State = classify(p)

		// Transceiver / DOM.
		ifc.HasTransceiver = p.HasTransceiver
		ifc.MediaType = p.MediaType
		ifc.XcvrVendor = p.XcvrVendor
		ifc.XcvrPart = p.XcvrPart
		ifc.XcvrSerial = p.XcvrSerial
		ifc.DomValid = p.DomValid
		ifc.TxPowerDbm = p.TxPowerDbm
		ifc.RxPowerDbm = p.RxPowerDbm
		ifc.TempC = p.TempC
		ifc.VoltageV = p.VoltageV
		ifc.OpticAlarm = opticAlarm(p)

		out = append(out, ifc)
	}
	return out
}

// classify maps admin/oper status into a port state.
func classify(p PortSample) string {
	switch p.OperStatus {
	case "errdisabled":
		return model.PortError
	}
	if p.AdminStatus == "down" {
		return model.PortDisabled
	}
	if p.OperStatus == "connected" || p.OperStatus == "up" {
		return model.PortUsed
	}
	return model.PortFree
}

// opticAlarm flags a degrading optic from its DOM readings. It returns an empty
// string when the optic is healthy or has no valid DOM data.
func opticAlarm(p PortSample) string {
	if !p.DomValid {
		return ""
	}
	switch {
	case p.RxPowerDbm < rxLowDbm:
		return "low-rx"
	case p.TxPowerDbm < txLowDbm:
		return "low-tx"
	case p.TempC > tempHighC:
		return "high-temp"
	}
	return ""
}

// rate computes bits/second from the octet delta between two counter samples.
// It returns 0 on the first sample or when a counter reset is detected.
func rate(prev, cur model.Counters) (in, out float64) {
	if prev.Timestamp.IsZero() {
		return 0, 0
	}
	dt := cur.Timestamp.Sub(prev.Timestamp).Seconds()
	if dt <= 0 {
		return 0, 0
	}
	if cur.InOctets >= prev.InOctets {
		in = float64(cur.InOctets-prev.InOctets) * 8 / dt
	}
	if cur.OutOctets >= prev.OutOctets {
		out = float64(cur.OutOctets-prev.OutOctets) * 8 / dt
	}
	return in, out
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
