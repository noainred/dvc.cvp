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
	"syscall"
	"time"

	"github.com/noainred/dvc.cvp/internal/api"
	"github.com/noainred/dvc.cvp/internal/collector"
	"github.com/noainred/dvc.cvp/internal/config"
	"github.com/noainred/dvc.cvp/internal/cvp"
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

	coll := collector.New(provider, st, cfg.Poll.Interval, cfg.Poll.Timeout)
	hub := api.NewHub(st)
	coll.OnUpdate(hub.Broadcast)

	upMgr := upgrade.New(cfg.Upgrade, version.Get())

	apiH := api.NewAPI(st, cfg.Mode, upMgr)
	srv := api.NewServer(cfg.Server.Listen, cfg.Server.WebDir, apiH, hub)

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

// newProvider selects the live CVP backend or the demo generator.
func newProvider(cfg *config.Config) (collector.Provider, error) {
	if cfg.Mode == config.ModeCVP {
		log.Print("collection mode: live CloudVision Portal")
		return cvp.NewProvider(cfg)
	}
	log.Print("collection mode: demo (synthetic fleet)")
	return collector.NewMock(), nil
}
