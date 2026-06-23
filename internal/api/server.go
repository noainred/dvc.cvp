package api

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/noainred/dvc.cvp/internal/auth"
)

// NewServer builds the HTTP server: REST API, the SSE stream and the static
// single-page frontend (with SPA fallback to index.html), behind the optional
// auth middleware.
func NewServer(addr, webDir string, a *API, hub *Hub, au *auth.Manager) *http.Server {
	mux := http.NewServeMux()
	a.Register(mux)
	mux.HandleFunc("GET /api/stream", hub.Handle)
	mux.Handle("/", spaHandler(webDir))

	return &http.Server{
		Addr:              addr,
		Handler:           logRequests(authMiddleware(au, mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// authMiddleware enforces access control when auth is enabled: login/auth/
// health and the static SPA are public; all other /api/* routes require a valid
// session, and admin-only routes (self-upgrade, audit log) require the admin
// role. When auth is disabled it is a transparent pass-through.
func authMiddleware(m *auth.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if p == "/api/login" || p == "/api/auth" || p == "/api/health" || !strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		_, role, ok := m.Resolve(auth.Token(r))
		if !ok {
			jsonError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if adminOnly(r) && role != auth.RoleAdmin {
			jsonError(w, http.StatusForbidden, "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminOnly(r *http.Request) bool {
	if r.URL.Path == "/api/audit" {
		return true
	}
	return r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/upgrade")
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

// spaHandler serves built frontend assets from dir and falls back to
// index.html for client-side routes. If the frontend has not been built yet it
// serves a small built-in status page instead.
func spaHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := path.Clean(r.URL.Path)
		if upath != "/" {
			full := filepath.Join(dir, filepath.FromSlash(upath))
			if rel, err := filepath.Rel(dir, full); err == nil && !strings.HasPrefix(rel, "..") {
				if st, err := os.Stat(full); err == nil && !st.IsDir() {
					fileServer.ServeHTTP(w, r)
					return
				}
			}
		}
		index := filepath.Join(dir, "index.html")
		if _, err := os.Stat(index); err == nil {
			http.ServeFile(w, r, index)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fallbackHTML))
	})
}

const fallbackHTML = `<!doctype html><html lang="ko"><head><meta charset="utf-8">
<title>dvc.cvp — Arista 모니터링</title>
<style>body{font-family:system-ui,sans-serif;background:#0b1220;color:#e2e8f0;margin:0;padding:48px;line-height:1.6}
code{background:#1e293b;padding:2px 6px;border-radius:4px}a{color:#38bdf8}
.card{max-width:760px;margin:auto;background:#111a2e;border:1px solid #1e293b;border-radius:12px;padding:32px}</style>
</head><body><div class="card">
<h1>dvc.cvp — Arista CloudVision 모니터링</h1>
<p>백엔드 API는 동작 중이지만 React 프론트엔드가 아직 빌드되지 않았습니다.</p>
<p>프론트엔드를 빌드하려면:</p>
<pre><code>cd web &amp;&amp; npm install &amp;&amp; npm run build</code></pre>
<p>API는 바로 사용할 수 있습니다:</p>
<ul>
<li><a href="/api/summary">/api/summary</a> — 전체 요약</li>
<li><a href="/api/datacenters">/api/datacenters</a> — 데이터센터 목록</li>
<li><a href="/api/devices">/api/devices</a> — 장비 목록</li>
<li><code>/api/stream</code> — 실시간 SSE 스트림</li>
</ul>
</div></body></html>`

// logRequests is lightweight access logging middleware.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't spam logs with the long-lived SSE connection.
		if r.URL.Path == "/api/stream" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush exposes the underlying flusher so SSE keeps working through the wrapper.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
