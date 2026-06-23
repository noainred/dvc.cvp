package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/noainred/dvc.cvp/internal/alert"
	"github.com/noainred/dvc.cvp/internal/model"
	"github.com/noainred/dvc.cvp/internal/store"
)

// livePayload is pushed to dashboards on every poll cycle. It carries the
// global summary plus per-data-center and per-device rollups and the active
// alerts; detailed per-interface data is fetched on demand via REST.
type livePayload struct {
	Summary     model.Summary      `json:"summary"`
	DataCenters []model.DataCenter `json:"datacenters"`
	Devices     []model.Device     `json:"devices"`
	Alerts      []model.Alert      `json:"alerts"`
}

// Hub fans out live snapshots to all connected SSE clients.
type Hub struct {
	store   *store.Store
	alerts  *alert.Engine
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// NewHub creates an SSE hub backed by the store and alert engine.
func NewHub(s *store.Store, a *alert.Engine) *Hub {
	return &Hub{store: s, alerts: a, clients: map[chan []byte]struct{}{}}
}

func (h *Hub) payload() []byte {
	p := livePayload{
		Summary:     h.store.Summary(),
		DataCenters: h.store.DataCenters(),
		Devices:     h.store.Devices(""),
		Alerts:      h.alerts.Active(),
	}
	data, _ := json.Marshal(p)
	frame := append([]byte("event: snapshot\ndata: "), data...)
	frame = append(frame, '\n', '\n')
	return frame
}

// Broadcast pushes the current store snapshot to every connected client. It is
// wired to the collector's OnUpdate callback.
func (h *Hub) Broadcast() {
	frame := h.payload()
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- frame:
		default: // slow client: drop this frame rather than block
		}
	}
}

// Handle is the SSE endpoint (GET /api/stream).
func (h *Hub) Handle(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	// Send the current state immediately so the UI paints without waiting for
	// the next poll cycle.
	if _, err := w.Write(h.payload()); err != nil {
		return
	}
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-ch:
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
