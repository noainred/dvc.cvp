// Command server is the dvc.cvp monitoring backend. It collects Arista switch
// telemetry from CloudVision Portal across many data centers (each reached
// through its own SSH-jump-host proxy), turns it into port-utilization and
// traffic metrics, and serves a REST + SSE API together with the React
// dashboard from a single binary.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/noainred/dvc.cvp/internal/alert"
	"github.com/noainred/dvc.cvp/internal/api"
	"github.com/noainred/dvc.cvp/internal/auth"
	"github.com/noainred/dvc.cvp/internal/collector"
	"github.com/noainred/dvc.cvp/internal/config"
	"github.com/noainred/dvc.cvp/internal/cvp"
	"github.com/noainred/dvc.cvp/internal/history"
	"github.com/noainred/dvc.cvp/internal/store"
	"github.com/noainred/dvc.cvp/internal/upgrade"
	"github.com/noainred/dvc.cvp/internal/version"
)

func main() {
	var (
		cfgPath = flag.String("config", "config/config.yaml", "path to YAML config file")
		listen  = flag.String("listen", "", "override server listen address (e.g. :8080)")
	)
	flag.Parse()

	cfg := loadConfig(*cfgPath)
	if *listen != "" {
		cfg.Server.Listen = *listen
	}

	st := store.New(cfg.Poll.HistoryPoints)

	provider, err := newProvider(cfg)
	if err != nil {
		log.Fatalf("init provider: %v", err)
	}

	alerts := alert.New(cfg.Alert, st)
	hist := openHistory(cfg.History)
	defer hist.Close()

	coll := collector.New(provider, st, cfg.Poll.Interval, cfg.Poll.Timeout)
	hub := api.NewHub(st, alerts)
	// After each poll: evaluate alerts, persist a history sample, then push the
	// snapshot (with the freshly-updated active alerts) to dashboards.
	coll.OnUpdate(func() {
		alerts.Evaluate()
		hist.Record(st.Summary(), st.Devices(""))
		hub.Broadcast()
	})

	upMgr := upgrade.New(cfg.Upgrade, version.Get())
	authMgr := auth.New(cfg.Auth)

	apiH := api.NewAPI(st, cfg.Mode, upMgr, alerts, hist, authMgr)
	srv := api.NewServer(cfg.Server.Listen, cfg.Server.WebDir, apiH, hub, authMgr)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go coll.Run(ctx)
	go upMgr.Run(ctx)

	go func() {
		<-ctx.Done()
		shutCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Printf("dvc.cvp %s listening on %s (mode=%s, poll=%s, sites=%d)",
		version.Version, cfg.Server.Listen, cfg.Mode, cfg.Poll.Interval, len(cfg.DataCenters))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
	log.Print("dvc.cvp stopped")
}

// loadConfig loads the config file, falling back to demo defaults if it does
// not exist so the binary runs out of the box.
func loadConfig(path string) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || os.IsNotExist(errors.Unwrap(err)) {
			log.Printf("config %q not found; starting in demo mode", path)
			return config.Default()
		}
		log.Fatalf("load config: %v", err)
	}
	return cfg
}

// openHistory opens the persistent history store, or returns a nil store
// (whose methods are no-ops) if disabled or unavailable.
func openHistory(cfg config.HistoryConfig) *history.Store {
	if !cfg.Enabled {
		return nil
	}
	if dir := filepath.Dir(cfg.Path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	h, err := history.Open(cfg.Path, cfg.Retention)
	if err != nil {
		log.Printf("history disabled: %v", err)
		return nil
	}
	log.Printf("history: persisting to %s (retention %s)", cfg.Path, cfg.Retention)
	return h
}

// newProvider selects the live CVP backend or the demo generator.
func newProvider(cfg *config.Config) (collector.Provider, error) {
	if cfg.Mode == config.ModeCVP {
		log.Print("collection mode: live CloudVision Portal")
		return cvp.NewProvider(cfg)
	}
	log.Print("collection mode: demo (synthetic fleet)")
	return collector.NewMock(), nil
}
