package cvp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/noainred/dvc.cvp/internal/collector"
	"github.com/noainred/dvc.cvp/internal/config"
	"github.com/noainred/dvc.cvp/internal/model"
	"github.com/noainred/dvc.cvp/internal/proxy"
)

// deviceConcurrency bounds simultaneous eAPI polls per data center.
const deviceConcurrency = 8

// dcRuntime is the live connection state for one data center.
type dcRuntime struct {
	cfg    config.DataCenterConfig
	dialer proxy.Dialer
	cvp    *Client
	eapi   *http.Client
}

// Provider is the live collector backend across every configured data center.
type Provider struct {
	dcs []*dcRuntime
}

// NewProvider wires a CVP client and eAPI HTTP client (both tunneled through
// the site proxy) for each configured data center.
func NewProvider(cfg *config.Config) (*Provider, error) {
	p := &Provider{}
	for _, dccfg := range cfg.DataCenters {
		dialer, err := proxy.New(dccfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("dc %s proxy: %w", dccfg.ID, err)
		}
		cvpc, err := NewClient(dccfg.CVP, dialer)
		if err != nil {
			return nil, fmt.Errorf("dc %s cvp: %w", dccfg.ID, err)
		}
		eapi := &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext:         dialer.DialContext,
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // switch certs are self-signed
				TLSHandshakeTimeout: 10 * time.Second,
				MaxIdleConns:        deviceConcurrency * 2,
				IdleConnTimeout:     90 * time.Second,
			},
		}
		p.dcs = append(p.dcs, &dcRuntime{cfg: dccfg, dialer: dialer, cvp: cvpc, eapi: eapi})
	}
	return p, nil
}

func (p *Provider) Name() string { return "cvp" }

// Poll collects inventory and interface telemetry from every data center in
// parallel. A site that is unreachable is reported with an error rather than
// failing the whole cycle.
func (p *Provider) Poll(ctx context.Context) (*collector.Snapshot, error) {
	snap := &collector.Snapshot{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, dc := range p.dcs {
		wg.Add(1)
		go func(dc *dcRuntime) {
			defer wg.Done()
			meta, devs := p.pollDC(ctx, dc)
			mu.Lock()
			snap.DataCenters = append(snap.DataCenters, meta)
			snap.Devices = append(snap.Devices, devs...)
			mu.Unlock()
		}(dc)
	}
	wg.Wait()
	return snap, nil
}

func (p *Provider) pollDC(ctx context.Context, dc *dcRuntime) (model.DataCenter, []collector.DeviceSample) {
	meta := model.DataCenter{
		ID: dc.cfg.ID, Name: dc.cfg.Name, Region: dc.cfg.Region, City: dc.cfg.City,
		ProxyType: dc.cfg.Proxy.Type, Status: model.StatusOK,
	}

	inv, err := dc.cvp.Inventory(ctx)
	if err != nil {
		meta.Status = model.StatusUnreachable
		meta.Error = err.Error()
		return meta, nil
	}

	sem := make(chan struct{}, deviceConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	samples := make([]collector.DeviceSample, 0, len(inv))

	for _, cd := range inv {
		dev := model.Device{
			Serial:     cd.SerialNumber,
			Hostname:   firstNonEmpty(cd.Hostname, cd.Fqdn, cd.SerialNumber),
			Model:      cd.ModelName,
			Family:     family(cd.ModelName),
			Version:    firstNonEmpty(cd.Version, cd.SoftwareVersion),
			MgmtIP:     cd.IPAddress,
			DataCenter: dc.cfg.ID,
			Role:       role(cd.ModelName),
			Status:     streamStatus(cd),
		}
		if dev.MgmtIP == "" {
			mu.Lock()
			samples = append(samples, collector.DeviceSample{Device: dev})
			mu.Unlock()
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(dev model.Device) {
			defer wg.Done()
			defer func() { <-sem }()
			ports, err := fetchInterfaces(ctx, dc.eapi, dev.MgmtIP,
				dc.cfg.CVP.DeviceUsername, dc.cfg.CVP.DevicePassword)
			if err != nil {
				dev.Status = model.StatusInactive
			}
			mu.Lock()
			samples = append(samples, collector.DeviceSample{Device: dev, Ports: ports})
			mu.Unlock()
		}(dev)
	}
	wg.Wait()
	return meta, samples
}

func (p *Provider) Close() error {
	for _, dc := range p.dcs {
		_ = dc.dialer.Close()
	}
	return nil
}

// family maps an Arista model string to one of the families we manage.
func family(model string) string {
	m := strings.ToUpper(model)
	switch {
	case strings.Contains(m, "7280"):
		return "7280"
	case strings.Contains(m, "7500"), strings.Contains(m, "7504"),
		strings.Contains(m, "7508"), strings.Contains(m, "7512"),
		strings.Contains(m, "7516"):
		return "7500"
	case strings.Contains(m, "7010"):
		return "7010"
	default:
		return "other"
	}
}

// role infers a topology role from the model family.
func role(model string) string {
	switch family(model) {
	case "7500":
		return "spine"
	case "7280":
		return "leaf"
	case "7010":
		return "mgmt"
	default:
		return ""
	}
}

func streamStatus(d cvpDevice) string {
	if strings.EqualFold(d.StreamingStatus, "active") || strings.EqualFold(d.Status, "active") {
		return model.StatusOK
	}
	return model.StatusInactive
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
