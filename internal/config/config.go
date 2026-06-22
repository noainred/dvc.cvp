// Package config loads the YAML configuration that describes the server, the
// collection mode (demo vs. live CVP), polling cadence and every monitored
// data center together with its CloudVision endpoint and proxy.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Collection modes.
const (
	ModeDemo = "demo" // synthetic data, no infrastructure required
	ModeCVP  = "cvp"  // live CloudVision Portal + eAPI counters via proxy
)

// Config is the root configuration document.
type Config struct {
	Server      ServerConfig       `yaml:"server"`
	Mode        string             `yaml:"mode"`
	Poll        PollConfig         `yaml:"poll"`
	Upgrade     UpgradeConfig      `yaml:"upgrade"`
	DataCenters []DataCenterConfig `yaml:"datacenters"`
}

// UpgradeConfig controls the portal's self-upgrade feature. The manager checks
// a GitHub releases endpoint for a newer build and can replace the running
// binary in place. AutoApply makes the upgrade happen automatically; otherwise
// availability is reported and the upgrade is triggered from the portal.
type UpgradeConfig struct {
	Enabled       bool          `yaml:"enabled"`       // allow checks and the upgrade endpoint
	AutoApply     bool          `yaml:"autoApply"`     // apply automatically when a newer release is found
	CheckInterval time.Duration `yaml:"checkInterval"` // periodic check cadence (0 disables)
	Repo          string        `yaml:"repo"`          // "owner/repo" for GitHub releases
	ReleaseURL    string        `yaml:"releaseUrl"`    // override the latest-release API URL (mirror/private)
	AssetPattern  string        `yaml:"assetPattern"`  // asset name pattern, {os}/{arch} expanded
	Token         string        `yaml:"token"`         // optional token for private release sources
}

// ServerConfig controls the HTTP listener and where the built frontend lives.
type ServerConfig struct {
	Listen string `yaml:"listen"`
	WebDir string `yaml:"webDir"`
}

// PollConfig controls how often telemetry is collected and how much history
// is retained per interface for charting.
type PollConfig struct {
	Interval      time.Duration `yaml:"interval"`
	Timeout       time.Duration `yaml:"timeout"`
	HistoryPoints int           `yaml:"historyPoints"`
}

// DataCenterConfig describes one monitored site.
type DataCenterConfig struct {
	ID     string      `yaml:"id"`
	Name   string      `yaml:"name"`
	Region string      `yaml:"region"`
	City   string      `yaml:"city"`
	CVP    CVPConfig   `yaml:"cvp"`
	Proxy  ProxyConfig `yaml:"proxy"`
}

// CVPConfig holds CloudVision Portal connection details for a site. Either a
// service-account Token or Username/Password may be used to authenticate.
// Device{Username,Password} are the EOS eAPI credentials used to read
// real-time interface counters from the switches discovered via CVP.
type CVPConfig struct {
	URL                string `yaml:"url"`
	Token              string `yaml:"token"`
	Username           string `yaml:"username"`
	Password           string `yaml:"password"`
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify"`
	DeviceUsername     string `yaml:"deviceUsername"`
	DevicePassword     string `yaml:"devicePassword"`
}

// ProxyConfig describes how to reach a remote site. "direct" dials normally;
// "ssh" tunnels every connection through an SSH jump host.
type ProxyConfig struct {
	Type       string `yaml:"type"`
	JumpHost   string `yaml:"jumpHost"`
	User       string `yaml:"user"`
	KeyFile    string `yaml:"keyFile"`
	Password   string `yaml:"password"`
	KnownHosts string `yaml:"knownHosts"`
}

// Default returns a configuration suitable for demo mode with no config file.
func Default() *Config {
	c := &Config{}
	c.applyDefaults()
	return c
}

// Load reads, parses and validates the configuration file at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = ":8080"
	}
	if c.Server.WebDir == "" {
		c.Server.WebDir = "./web/dist"
	}
	if c.Mode == "" {
		c.Mode = ModeDemo
	}
	if c.Poll.Interval <= 0 {
		c.Poll.Interval = 10 * time.Second
	}
	if c.Poll.Timeout <= 0 {
		c.Poll.Timeout = 8 * time.Second
	}
	if c.Poll.HistoryPoints <= 0 {
		c.Poll.HistoryPoints = 360
	}
	if c.Upgrade.Repo == "" {
		c.Upgrade.Repo = "noainred/dvc.cvp"
	}
	if c.Upgrade.AssetPattern == "" {
		c.Upgrade.AssetPattern = "dvc-cvp_{os}_{arch}"
	}
	if c.Upgrade.CheckInterval == 0 {
		c.Upgrade.CheckInterval = 6 * time.Hour
	}
	for i := range c.DataCenters {
		if c.DataCenters[i].Proxy.Type == "" {
			c.DataCenters[i].Proxy.Type = "direct"
		}
	}
}

func (c *Config) validate() error {
	switch c.Mode {
	case ModeDemo, ModeCVP:
	default:
		return fmt.Errorf("invalid mode %q (want %q or %q)", c.Mode, ModeDemo, ModeCVP)
	}
	if c.Mode == ModeCVP && len(c.DataCenters) == 0 {
		return fmt.Errorf("mode %q requires at least one datacenter", ModeCVP)
	}
	seen := map[string]bool{}
	for _, dc := range c.DataCenters {
		if dc.ID == "" {
			return fmt.Errorf("datacenter with empty id")
		}
		if seen[dc.ID] {
			return fmt.Errorf("duplicate datacenter id %q", dc.ID)
		}
		seen[dc.ID] = true
		if c.Mode == ModeCVP && dc.CVP.URL == "" {
			return fmt.Errorf("datacenter %q: cvp.url required in cvp mode", dc.ID)
		}
	}
	return nil
}
