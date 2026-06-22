package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/noainred/dvc.cvp/internal/store"
	"github.com/noainred/dvc.cvp/internal/upgrade"
	"github.com/noainred/dvc.cvp/internal/version"
)

// API holds the REST handlers backed by the store and upgrade manager.
type API struct {
	store   *store.Store
	mode    string
	upgrade *upgrade.Manager
}

// NewAPI creates the REST handler set.
func NewAPI(s *store.Store, mode string, up *upgrade.Manager) *API {
	return &API{store: s, mode: mode, upgrade: up}
}

// Register attaches all REST routes to the mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/version", a.version)
	mux.HandleFunc("POST /api/upgrade/check", a.upgradeCheck)
	mux.HandleFunc("POST /api/upgrade", a.upgradeApply)
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
