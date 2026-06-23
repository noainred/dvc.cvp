// Package cvp implements the live collection backend. Device inventory,
// topology (containers) and compliance come from the CloudVision Portal REST
// API; real-time interface counters are read from each switch via EOS eAPI
// using the management address learned from CVP. Every HTTP call is dialed
// through the data center's proxy (typically an SSH jump host), so the Korea
// HQ collector reaches each remote site through its bastion.
package cvp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/noainred/dvc.cvp/internal/config"
	"github.com/noainred/dvc.cvp/internal/proxy"
)

// Client talks to a single CloudVision Portal instance.
type Client struct {
	base string
	cfg  config.CVPConfig
	http *http.Client

	mu     sync.Mutex
	authed bool
}

// NewClient builds a CVP REST client whose connections are tunneled through the
// supplied dialer.
func NewClient(cfg config.CVPConfig, dialer proxy.Dialer) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify}, //nolint:gosec
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        50,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		base: strings.TrimRight(cfg.URL, "/"),
		cfg:  cfg,
		http: &http.Client{Transport: tr, Jar: jar, Timeout: 30 * time.Second},
	}, nil
}

// login authenticates with username/password and stores the session cookie.
// When a service-account token is configured this is a no-op (the token is sent
// as a bearer header on every request instead).
func (c *Client) login(ctx context.Context) error {
	if c.cfg.Token != "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authed {
		return nil
	}
	body, _ := json.Marshal(map[string]string{
		"userId":   c.cfg.Username,
		"password": c.cfg.Password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/cvpservice/login/authenticate.do", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cvp login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("cvp login: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	c.authed = true
	return nil
}

// get performs an authenticated GET and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	if err := c.login(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.authed = false
		c.mu.Unlock()
		return fmt.Errorf("cvp %s: unauthorized", path)
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("cvp %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// cvpDevice mirrors the subset of /cvpservice/inventory/devices we use. Field
// names differ slightly across CVP releases, so several aliases are accepted.
type cvpDevice struct {
	SerialNumber       string `json:"serialNumber"`
	SystemMac          string `json:"systemMacAddress"`
	Hostname           string `json:"hostname"`
	Fqdn               string `json:"fqdn"`
	ModelName          string `json:"modelName"`
	Version            string `json:"version"`
	SoftwareVersion    string `json:"softwareVersion"`
	IPAddress          string `json:"ipAddress"`
	StreamingStatus    string `json:"streamingStatus"`
	Status             string `json:"status"`
	ParentContainerKey string `json:"parentContainerKey"`
	ComplianceCode     string `json:"complianceCode"`
}

// Inventory returns all devices known to this CVP instance.
func (c *Client) Inventory(ctx context.Context) ([]cvpDevice, error) {
	// Newer CVP returns a bare array; older releases wrap it in {"data":[...]}.
	var arr []cvpDevice
	if err := c.get(ctx, "/cvpservice/inventory/devices", &arr); err == nil {
		return arr, nil
	}
	var wrapped struct {
		Data []cvpDevice `json:"data"`
	}
	if err := c.get(ctx, "/cvpservice/inventory/devices", &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}
