package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/noainred/dvc.cvp/internal/alert"
	"github.com/noainred/dvc.cvp/internal/flows"
	"github.com/noainred/dvc.cvp/internal/history"
	"github.com/noainred/dvc.cvp/internal/lanz"
	"github.com/noainred/dvc.cvp/internal/model"
	"github.com/noainred/dvc.cvp/internal/store"
	"github.com/noainred/dvc.cvp/internal/upgrade"
	"github.com/noainred/dvc.cvp/internal/version"
)

// API holds the REST handlers backed by the store, upgrade manager, alerts and
// the long-term history store.
type API struct {
	store   *store.Store
	mode    string
	upgrade *upgrade.Manager
	alerts  *alert.Engine
	history *history.Store
}

// NewAPI creates the REST handler set.
func NewAPI(s *store.Store, mode string, up *upgrade.Manager, al *alert.Engine, hist *history.Store) *API {
	return &API{store: s, mode: mode, upgrade: up, alerts: al, history: hist}
}

// Register attaches all REST routes to the mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/version", a.version)
	mux.HandleFunc("POST /api/upgrade/check", a.upgradeCheck)
	mux.HandleFunc("POST /api/upgrade", a.upgradeApply)
	mux.HandleFunc("GET /api/alerts", a.alertsList)
	mux.HandleFunc("GET /api/compliance", a.compliance)
	mux.HandleFunc("GET /api/reclaim", a.reclaim)
	mux.HandleFunc("GET /api/topology", a.topology)
	mux.HandleFunc("GET /api/flows", a.flowsTop)
	mux.HandleFunc("GET /api/congestion", a.congestion)
	mux.HandleFunc("GET /api/trend", a.trend)
	mux.HandleFunc("GET /api/devices/{serial}/trend", a.deviceTrend)
	mux.HandleFunc("GET /api/summary", a.summary)
	mux.HandleFunc("GET /api/datacenters", a.datacenters)
	mux.HandleFunc("GET /api/datacenters/{id}/devices", a.dcDevices)
	mux.HandleFunc("GET /api/devices", a.devices)
	mux.HandleFunc("GET /api/devices/{serial}", a.device)
	mux.HandleFunc("GET /api/devices/{serial}/interfaces", a.interfaces)
	mux.HandleFunc("GET /api/devices/{serial}/history", a.deviceHistory)
	mux.HandleFunc("GET /api/devices/{serial}/interface-history", a.interfaceHistory)
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"mode":    a.mode,
		"version": version.Version,
	})
}

// version returns the current build and upgrade availability for the portal.
func (a *API) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.upgrade.Status())
}

// upgradeCheck forces a release check and returns the refreshed status.
func (a *API) upgradeCheck(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := a.upgrade.Check(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, st)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// upgradeApply downloads and installs the newest release, then re-execs. The
// response is sent before the process restarts.
func (a *API) upgradeApply(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := a.upgrade.Apply(ctx); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "upgrading",
		"detail": "새 버전을 설치했습니다. 서버가 곧 재시작됩니다.",
	})
}

func (a *API) summary(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Summary())
}

// compliance returns the EOS version inventory and configuration-compliance
// rollup (per-version counts + the list of non-compliant devices).
func (a *API) compliance(w http.ResponseWriter, _ *http.Request) {
	type vrow struct {
		Version      string `json:"version"`
		Count        int    `json:"count"`
		NonCompliant int    `json:"nonCompliant"`
	}
	byVer := map[string]*vrow{}
	var non []model.Device
	devs := a.store.Devices("")
	for _, d := range devs {
		v := byVer[d.Version]
		if v == nil {
			v = &vrow{Version: d.Version}
			byVer[d.Version] = v
		}
		v.Count++
		if d.Compliance == model.ComplianceNon {
			v.NonCompliant++
			non = append(non, d)
		}
	}
	rows := make([]vrow, 0, len(byVer))
	for _, v := range byVer {
		rows = append(rows, *v)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Version < rows[j].Version })
	sort.Slice(non, func(i, j int) bool {
		if non[i].DataCenter != non[j].DataCenter {
			return non[i].DataCenter < non[j].DataCenter
		}
		return non[i].Hostname < non[j].Hostname
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"total":        len(devs),
		"nonCompliant": len(non),
		"byVersion":    rows,
		"devices":      non,
	})
}

// reclaim returns free (unused) ports idle for at least ?days (default 30),
// most-idle first — candidates for reclamation/repurposing.
func (a *API) reclaim(w http.ResponseWriter, r *http.Request) {
	minDays := floatParam(r, "days", 30)
	now := time.Now()
	out := []model.ReclaimPort{}
	for _, d := range a.store.Devices("") {
		for _, ifc := range a.store.Interfaces(d.Serial) {
			if ifc.State != model.PortFree {
				continue
			}
			idle := 0.0
			if !ifc.LastChange.IsZero() {
				idle = now.Sub(ifc.LastChange).Hours() / 24
			}
			if idle < minDays {
				continue
			}
			out = append(out, model.ReclaimPort{
				DataCenter: d.DataCenter, Device: d.Serial, Hostname: d.Hostname,
				Interface: ifc.Name, HasTransceiver: ifc.HasTransceiver, MediaType: ifc.MediaType,
				SpeedBps: ifc.SpeedBps, LastChange: ifc.LastChange, IdleDays: idle,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IdleDays > out[j].IdleDays })
	if len(out) > 1000 {
		out = out[:1000]
	}
	writeJSON(w, http.StatusOK, out)
}

// topology returns the LLDP-derived device graph (nodes + adjacencies) for the
// optionally filtered data center.
func (a *API) topology(w http.ResponseWriter, r *http.Request) {
	devs := a.store.Devices(r.URL.Query().Get("dc"))
	byHost := map[string]string{} // hostname -> serial
	nodes := make([]model.TopologyNode, 0, len(devs))
	for _, d := range devs {
		byHost[d.Hostname] = d.Serial
		nodes = append(nodes, model.TopologyNode{
			ID: d.Serial, Hostname: d.Hostname, Role: d.Role, Status: d.Status, DataCenter: d.DataCenter,
		})
	}
	linkMap := map[string]*model.TopologyLink{}
	for _, d := range devs {
		for _, ifc := range a.store.Interfaces(d.Serial) {
			peer := byHost[ifc.Neighbor]
			if peer == "" || peer == d.Serial {
				continue
			}
			x, y := d.Serial, peer
			if x > y {
				x, y = y, x
			}
			key := x + "|" + y
			l := linkMap[key]
			if l == nil {
				l = &model.TopologyLink{A: x, B: y}
				linkMap[key] = l
			}
			l.Count++
		}
	}
	links := make([]model.TopologyLink, 0, len(linkMap))
	for _, l := range linkMap {
		if l.Count > 1 {
			l.Count /= 2 // each cable is reported from both ends
		}
		links = append(links, *l)
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "links": links})
}

// flowsTop returns the busiest flows (sFlow/IPFIX-style top talkers).
func (a *API) flowsTop(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	writeJSON(w, http.StatusOK, flows.Top(a.store, q.Get("dc"), q.Get("device"), intParam(r, "limit", 20)))
}

// congestion returns LANZ-style queue-congestion / microburst events.
func (a *API) congestion(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	writeJSON(w, http.StatusOK, lanz.Events(a.store, q.Get("dc"), q.Get("device"), intParam(r, "limit", 50)))
}

// alertsList returns the active alerts and recent alert events.
func (a *API) alertsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"active": a.alerts.Active(),
		"recent": a.alerts.Recent(),
	})
}

func (a *API) datacenters(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.store.DataCenters())
}

func (a *API) dcDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Devices(r.PathValue("id")))
}

func (a *API) devices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Devices(r.URL.Query().Get("dc")))
}

func (a *API) device(w http.ResponseWriter, r *http.Request) {
	d, ok := a.store.Device(r.PathValue("serial"))
	if !ok {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *API) interfaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.store.Interfaces(r.PathValue("serial")))
}

func (a *API) deviceHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.store.DeviceHistory(r.PathValue("serial")))
}

// trend returns the global long-term port-usage / throughput series plus a
// linear capacity projection (default: when port usage reaches 80%).
func (a *API) trend(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-rangeDuration(r, 30))
	pts := a.history.SummarySeries(since)
	threshold := floatParam(r, "threshold", 80)
	writeJSON(w, http.StatusOK, map[string]any{
		"series":     pts,
		"portUsage":  history.PortUsageTrend(pts, threshold),
		"throughput": history.ThroughputTrend(pts),
	})
}

// deviceTrend returns a device's long-term throughput series and growth trend.
func (a *API) deviceTrend(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-rangeDuration(r, 30))
	samples := a.history.DeviceSeries(r.PathValue("serial"), since)
	writeJSON(w, http.StatusOK, map[string]any{
		"series":     samples,
		"throughput": history.DeviceTrend(samples),
	})
}

// rangeDuration reads ?days=N (default defDays), clamped to [1, 365].
func rangeDuration(r *http.Request, defDays int) time.Duration {
	d := defDays
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 365 {
			d = n
		}
	}
	return time.Duration(d) * 24 * time.Hour
}

func intParam(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func floatParam(r *http.Request, name string, def float64) float64 {
	if v := r.URL.Query().Get(name); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func (a *API) interfaceHistory(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing 'name' query parameter")
		return
	}
	writeJSON(w, http.StatusOK, a.store.InterfaceHistory(r.PathValue("serial"), name))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
