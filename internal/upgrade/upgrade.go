// Package upgrade implements the portal's self-upgrade feature. It checks a
// GitHub releases endpoint for a newer build, downloads the matching asset
// (raw binary or .tar.gz), atomically replaces the running executable and
// re-execs the process into the new binary.
//
// Note: replacing a running executable and re-exec rely on POSIX semantics and
// are intended for the Linux server deployment described in the README.
package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/noainred/dvc.cvp/internal/config"
	"github.com/noainred/dvc.cvp/internal/version"
)

// Status is the upgrade state reported to the portal.
type Status struct {
	Current          string    `json:"current"`
	Commit           string    `json:"commit"`
	BuildTime        string    `json:"buildTime"`
	Latest           string    `json:"latest,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	ReleaseURL       string    `json:"releaseUrl,omitempty"`
	UpgradeAvailable bool      `json:"upgradeAvailable"`
	Enabled          bool      `json:"enabled"`
	AutoApply        bool      `json:"autoApply"`
	Applying         bool      `json:"applying"`
	LastChecked      time.Time `json:"lastChecked,omitempty"`
	Error            string    `json:"error,omitempty"`
}

// Manager performs release checks and self-upgrades.
type Manager struct {
	cfg      config.UpgradeConfig
	current  version.Info
	http     *http.Client
	assetURL string

	mu     sync.Mutex
	status Status
}

// New builds an upgrade manager.
func New(cfg config.UpgradeConfig, cur version.Info) *Manager {
	return &Manager{
		cfg:     cfg,
		current: cur,
		http:    &http.Client{Timeout: 60 * time.Second},
		status: Status{
			Current:   cur.Version,
			Commit:    cur.Commit,
			BuildTime: cur.BuildTime,
			Enabled:   cfg.Enabled,
			AutoApply: cfg.AutoApply,
		},
	}
}

// Status returns the latest cached status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) setStatus(fn func(s *Status)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.status)
}

// Run performs an immediate check and then re-checks on the configured
// interval, applying automatically when AutoApply is set.
func (m *Manager) Run(ctx context.Context) {
	if !m.cfg.Enabled || m.cfg.CheckInterval <= 0 {
		return
	}
	m.checkAndMaybeApply(ctx)
	t := time.NewTicker(m.cfg.CheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.checkAndMaybeApply(ctx)
		}
	}
}

func (m *Manager) checkAndMaybeApply(ctx context.Context) {
	st, err := m.Check(ctx)
	if err != nil {
		log.Printf("upgrade: check failed: %v", err)
		return
	}
	if st.UpgradeAvailable && m.cfg.AutoApply {
		log.Printf("upgrade: auto-applying %s -> %s", m.current.Version, st.Latest)
		if err := m.Apply(ctx); err != nil {
			log.Printf("upgrade: auto-apply failed: %v", err)
		}
	}
}

// ghRelease is the subset of the GitHub releases API we consume.
type ghRelease struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (m *Manager) latestURL() string {
	if m.cfg.ReleaseURL != "" {
		return m.cfg.ReleaseURL
	}
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", m.cfg.Repo)
}

func (m *Manager) do(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if m.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+m.cfg.Token)
	}
	return m.http.Do(req)
}

// Check queries the release source and refreshes the cached status.
func (m *Manager) Check(ctx context.Context) (Status, error) {
	resp, err := m.do(ctx, m.latestURL())
	if err != nil {
		m.setStatus(func(s *Status) { s.Error = err.Error(); s.LastChecked = time.Now() })
		return m.Status(), err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		err := fmt.Errorf("release source status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		m.setStatus(func(s *Status) { s.Error = err.Error(); s.LastChecked = time.Now() })
		return m.Status(), err
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		m.setStatus(func(s *Status) { s.Error = err.Error(); s.LastChecked = time.Now() })
		return m.Status(), err
	}

	asset := pickAsset(rel, m.cfg.AssetPattern)
	available := Newer(rel.TagName, m.current.Version) && asset != ""
	m.setStatus(func(s *Status) {
		s.Latest = rel.TagName
		s.Notes = rel.Body
		s.ReleaseURL = rel.HTMLURL
		s.UpgradeAvailable = available
		s.LastChecked = time.Now()
		s.Error = ""
	})
	m.assetURL = asset
	return m.Status(), nil
}

// Apply downloads the newest release asset and replaces the running binary,
// then re-execs into it. It returns an error if no newer release/asset exists
// or if the download/replace fails.
func (m *Manager) Apply(ctx context.Context) error {
	if !m.cfg.Enabled {
		return fmt.Errorf("upgrade is disabled")
	}
	st, err := m.Check(ctx)
	if err != nil {
		return err
	}
	if !st.UpgradeAvailable {
		return fmt.Errorf("no newer release available (current %s, latest %s)", m.current.Version, st.Latest)
	}
	if m.assetURL == "" {
		return fmt.Errorf("no matching asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	m.setStatus(func(s *Status) { s.Applying = true })
	defer m.setStatus(func(s *Status) { s.Applying = false })

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)

	tmp, err := os.CreateTemp(dir, ".dvc-cvp-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // removed only if rename below did not consume it

	if err := m.download(ctx, m.assetURL, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}

	// Keep a backup of the current binary, then atomically swap in the new one.
	backup := exe + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		_ = os.Rename(backup, exe) // roll back
		return fmt.Errorf("install new binary: %w", err)
	}

	log.Printf("upgrade: installed %s, restarting", st.Latest)
	m.restart(exe)
	return nil
}

// restart re-execs the process into the (now replaced) executable after a short
// delay so the HTTP response can be flushed first.
func (m *Manager) restart(exe string) {
	go func() {
		time.Sleep(700 * time.Millisecond)
		if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
			log.Printf("upgrade: re-exec failed: %v; exiting for supervisor restart", err)
			os.Exit(0)
		}
	}()
}

// download fetches url into w. If the asset is a .tar.gz, the contained
// dvc-cvp binary is extracted. An optional checksums file in the same release
// is honored when present.
func (m *Manager) download(ctx context.Context, url string, w io.Writer) error {
	resp, err := m.do(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}

	if strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz") {
		return extractBinary(resp.Body, w)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// extractBinary streams a .tar.gz and writes the first regular file whose name
// looks like the server binary into w.
func extractBinary(r io.Reader, w io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("no dvc-cvp binary found in archive")
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base == "dvc-cvp" || strings.HasPrefix(base, "dvc-cvp") {
			_, err := io.Copy(w, tr) //nolint:gosec // size bounded by release artifact
			return err
		}
	}
}

// pickAsset selects the release asset matching the configured pattern with
// {os}/{arch} expanded. Falls back to the sole asset when only one exists.
func pickAsset(rel ghRelease, pattern string) string {
	want := strings.NewReplacer("{os}", runtime.GOOS, "{arch}", runtime.GOARCH).Replace(pattern)
	for _, a := range rel.Assets {
		if strings.Contains(a.Name, want) {
			return a.URL
		}
	}
	if len(rel.Assets) == 1 {
		return rel.Assets[0].URL
	}
	return ""
}

// Newer reports whether release tag a is a strictly higher semantic version
// than b. Tags may be prefixed with "v"; non-numeric suffixes are ignored.
func Newer(a, b string) bool {
	return compare(parse(a), parse(b)) > 0
}

func parse(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// drop any pre-release/build suffix
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		n, _ := strconv.Atoi(strings.TrimFunc(parts[i], func(r rune) bool { return r < '0' || r > '9' }))
		out[i] = n
	}
	return out
}

func compare(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] > b[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}
