package api

import (
	"encoding/json"
	"net/http"

	"github.com/noainred/dvc.cvp/internal/store"
)

// API holds the REST handlers backed by the store.
type API struct {
	store *store.Store
	mode  string
}

// NewAPI creates the REST handler set.
func NewAPI(s *store.Store, mode string) *API {
	return &API{store: s, mode: mode}
}

// Register attaches all REST routes to the mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", a.health)
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
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "mode": a.mode})
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
