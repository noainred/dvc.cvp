// Package auth provides optional role-based access control and an audit log.
// When disabled (demo default) every request is treated as admin. When enabled,
// users authenticate with username/password (plaintext or bcrypt hash) and
// receive a bearer token; only admins may self-upgrade the portal or read the
// audit log. Sensitive actions are recorded to an in-memory audit ring.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/noainred/dvc.cvp/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// Roles.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

const (
	sessionTTL = 12 * time.Hour
	auditMax   = 500
	cookieName = "dvc_token"
)

// Entry is one audit-log record.
type Entry struct {
	Time   time.Time `json:"time"`
	User   string    `json:"user"`
	Action string    `json:"action"`
	Detail string    `json:"detail,omitempty"`
	IP     string    `json:"ip,omitempty"`
	Result string    `json:"result"`
}

type userRec struct{ pass, role string }
type session struct {
	user, role string
	exp        time.Time
}

// Manager holds users, sessions and the audit ring.
type Manager struct {
	enabled bool
	users   map[string]userRec

	mu       sync.Mutex
	sessions map[string]session
	audit    []Entry
}

// New builds the auth manager from config.
func New(cfg config.AuthConfig) *Manager {
	m := &Manager{
		enabled:  cfg.Enabled,
		users:    map[string]userRec{},
		sessions: map[string]session{},
	}
	for _, u := range cfg.Users {
		role := u.Role
		if role != RoleAdmin && role != RoleViewer {
			role = RoleViewer
		}
		m.users[u.Username] = userRec{pass: u.Password, role: role}
	}
	return m
}

// Enabled reports whether access control is on.
func (m *Manager) Enabled() bool { return m.enabled }

// Login validates credentials and issues a session token.
func (m *Manager) Login(user, pass, ip string) (token, role string, err error) {
	rec, ok := m.users[user]
	if !ok || !checkPass(rec.pass, pass) {
		m.Record(user, "login", "", ip, "fail")
		return "", "", errors.New("invalid credentials")
	}
	token = randToken()
	m.mu.Lock()
	m.sessions[token] = session{user: user, role: rec.role, exp: time.Now().Add(sessionTTL)}
	m.mu.Unlock()
	m.Record(user, "login", "", ip, "ok")
	return token, rec.role, nil
}

// Logout invalidates a token.
func (m *Manager) Logout(token, ip string) {
	m.mu.Lock()
	s, ok := m.sessions[token]
	delete(m.sessions, token)
	m.mu.Unlock()
	if ok {
		m.Record(s.user, "logout", "", ip, "ok")
	}
}

// Resolve returns the user/role for a token, or ok=false if invalid/expired.
func (m *Manager) Resolve(token string) (user, role string, ok bool) {
	if token == "" {
		return "", "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[token]
	if !ok || time.Now().After(s.exp) {
		if ok {
			delete(m.sessions, token)
		}
		return "", "", false
	}
	return s.user, s.role, true
}

// Record appends an audit entry (bounded ring).
func (m *Manager) Record(user, action, detail, ip, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, Entry{
		Time: time.Now(), User: user, Action: action, Detail: detail, IP: ip, Result: result,
	})
	if len(m.audit) > auditMax {
		m.audit = m.audit[len(m.audit)-auditMax:]
	}
}

// Audit returns recent audit entries, newest first.
func (m *Manager) Audit() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Entry, len(m.audit))
	for i, e := range m.audit {
		out[len(m.audit)-1-i] = e
	}
	return out
}

// Token extracts the bearer token from the Authorization header, the
// dvc_token cookie, or a ?token= query parameter (used by the SSE stream).
func Token(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("token")
}

// SetCookie sets the session cookie after a successful login.
func SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: false,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
}

// ClearCookie removes the session cookie on logout.
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
}

func checkPass(stored, given string) bool {
	if strings.HasPrefix(stored, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(given)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(given)) == 1
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
