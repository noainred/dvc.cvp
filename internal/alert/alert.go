// Package alert evaluates the fleet snapshot after every poll cycle and raises
// threshold/health alerts: high port utilization, optical (DOM) alarms, rising
// interface errors, devices that stopped streaming and unreachable data
// centers. Active alerts are exposed to the API/UI and, when a webhook is
// configured, raised/cleared transitions are pushed to it (Slack-compatible).
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/noainred/dvc.cvp/internal/config"
	"github.com/noainred/dvc.cvp/internal/model"
	"github.com/noainred/dvc.cvp/internal/store"
)

const recentMax = 200

// Engine holds alert state across poll cycles.
type Engine struct {
	cfg   config.AlertConfig
	store *store.Store
	http  *http.Client

	mu     sync.RWMutex
	active map[string]model.Alert
	recent []model.Alert // ring of recent raised/cleared events (newest last)

	prevErrors map[string]int64 // serial|iface -> last total errors+discards
}

// New creates an alert engine.
func New(cfg config.AlertConfig, s *store.Store) *Engine {
	return &Engine{
		cfg:        cfg,
		store:      s,
		http:       &http.Client{Timeout: 8 * time.Second},
		active:     map[string]model.Alert{},
		prevErrors: map[string]int64{},
	}
}

// Active returns the currently-active alerts, most severe / newest first.
func (e *Engine) Active() []model.Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]model.Alert, 0, len(e.active))
	for _, a := range e.active {
		out = append(out, a)
	}
	sortAlerts(out)
	return out
}

// Recent returns recent alert events (raised and cleared), newest first.
func (e *Engine) Recent() []model.Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]model.Alert, len(e.recent))
	for i, a := range e.recent {
		out[len(e.recent)-1-i] = a
	}
	return out
}

// ActiveCount returns the number of active alerts.
func (e *Engine) ActiveCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.active)
}

// Evaluate recomputes alerts from the current store snapshot. It is wired to
// the collector's OnUpdate callback and runs once per poll cycle.
func (e *Engine) Evaluate() {
	if !e.cfg.Enabled {
		return
	}
	now := time.Now()
	found := map[string]model.Alert{}

	for _, dc := range e.store.DataCenters() {
		if dc.Status == model.StatusUnreachable {
			add(found, model.Alert{
				Key: "dc:" + dc.ID, Severity: model.SevCritical, Type: "datacenter",
				DataCenter: dc.ID, Message: fmt.Sprintf("데이터센터 연결 불가: %s", dc.Name),
			})
		}
	}

	for _, d := range e.store.Devices("") {
		if d.Status == model.StatusUnreachable || d.Status == model.StatusInactive {
			add(found, model.Alert{
				Key: "device:" + d.Serial, Severity: model.SevCritical, Type: "device",
				DataCenter: d.DataCenter, Device: d.Serial, Hostname: d.Hostname,
				Message: fmt.Sprintf("장비 텔레메트리 중단: %s (%s)", d.Hostname, d.Status),
			})
		}
		for _, ifc := range e.store.Interfaces(d.Serial) {
			e.evalInterface(found, d, ifc)
		}
	}

	e.reconcile(found, now)
}

func (e *Engine) evalInterface(found map[string]model.Alert, d model.Device, ifc model.Interface) {
	// Utilization.
	if ifc.State == model.PortUsed && ifc.UtilPct >= e.cfg.UtilWarnPct {
		sev := model.SevWarning
		if ifc.UtilPct >= e.cfg.UtilCritPct {
			sev = model.SevCritical
		}
		add(found, model.Alert{
			Key: "util:" + d.Serial + ":" + ifc.Name, Severity: sev, Type: "util",
			DataCenter: d.DataCenter, Device: d.Serial, Hostname: d.Hostname, Interface: ifc.Name,
			Value:   ifc.UtilPct,
			Message: fmt.Sprintf("%s %s 사용률 %.0f%%", d.Hostname, ifc.Name, ifc.UtilPct),
		})
	}

	// Optical (DOM) alarm.
	if ifc.OpticAlarm != "" {
		add(found, model.Alert{
			Key: "optic:" + d.Serial + ":" + ifc.Name, Severity: model.SevWarning, Type: "optic",
			DataCenter: d.DataCenter, Device: d.Serial, Hostname: d.Hostname, Interface: ifc.Name,
			Value:   ifc.RxPowerDbm,
			Message: fmt.Sprintf("%s %s 광 경보(%s) Rx %.1f dBm", d.Hostname, ifc.Name, ifc.OpticAlarm, ifc.RxPowerDbm),
		})
	}

	// Rising errors/discards (delta since last poll).
	ekey := d.Serial + "|" + ifc.Name
	total := ifc.InErrors + ifc.OutErrors + ifc.InDiscards + ifc.OutDiscards
	prev, seen := e.prevErrors[ekey]
	e.prevErrors[ekey] = total
	if seen && total-prev >= e.cfg.ErrorsPerInterval {
		add(found, model.Alert{
			Key: "errors:" + d.Serial + ":" + ifc.Name, Severity: model.SevWarning, Type: "errors",
			DataCenter: d.DataCenter, Device: d.Serial, Hostname: d.Hostname, Interface: ifc.Name,
			Value:   float64(total - prev),
			Message: fmt.Sprintf("%s %s 에러/폐기 급증 (+%d)", d.Hostname, ifc.Name, total-prev),
		})
	}
}

// reconcile diffs the freshly-found alerts against the active set, preserving
// the Since timestamp of continuing alerts and dispatching transitions.
func (e *Engine) reconcile(found map[string]model.Alert, now time.Time) {
	e.mu.Lock()
	var raised, cleared []model.Alert
	for k, a := range found {
		if old, ok := e.active[k]; ok {
			a.Since = old.Since
		} else {
			a.Since = now
			a.Active = true
			raised = append(raised, a)
		}
		a.Active = true
		e.active[k] = a
	}
	for k, a := range e.active {
		if _, ok := found[k]; !ok {
			a.Active = false
			cleared = append(cleared, a)
			delete(e.active, k)
		}
	}
	for _, a := range raised {
		e.pushRecent(a)
	}
	for _, a := range cleared {
		e.pushRecent(a)
	}
	e.mu.Unlock()

	for _, a := range raised {
		e.notify(a, true)
	}
	if e.cfg.Resolve {
		for _, a := range cleared {
			e.notify(a, false)
		}
	}
}

// pushRecent appends to the bounded recent-events ring (caller holds the lock).
func (e *Engine) pushRecent(a model.Alert) {
	e.recent = append(e.recent, a)
	if len(e.recent) > recentMax {
		e.recent = e.recent[len(e.recent)-recentMax:]
	}
}

// notify posts a Slack-compatible message to the configured webhook.
func (e *Engine) notify(a model.Alert, raised bool) {
	if e.cfg.WebhookURL == "" {
		return
	}
	prefix := "🟢 해제"
	if raised {
		prefix = "🔴 발생"
		if a.Severity == model.SevWarning {
			prefix = "🟠 발생"
		}
	}
	text := fmt.Sprintf("[%s] %s — %s", a.Severity, prefix, a.Message)
	body, _ := json.Marshal(map[string]string{"text": text})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.WebhookURL, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := e.http.Do(req)
		if err != nil {
			log.Printf("alert: webhook failed: %v", err)
			return
		}
		_ = resp.Body.Close()
	}()
}

func add(m map[string]model.Alert, a model.Alert) { m[a.Key] = a }

func sortAlerts(a []model.Alert) {
	sort.Slice(a, func(i, j int) bool {
		if a[i].Severity != a[j].Severity {
			return a[i].Severity == model.SevCritical // critical first
		}
		return a[i].Since.After(a[j].Since)
	})
}
