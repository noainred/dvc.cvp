package cvp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/noainred/dvc.cvp/internal/collector"
	"github.com/noainred/dvc.cvp/internal/model"
)

// eapiReq is the JSON-RPC envelope EOS eAPI expects on /command-api.
type eapiReq struct {
	JSONRPC string    `json:"jsonrpc"`
	Method  string    `json:"method"`
	Params  eapiParam `json:"params"`
	ID      string    `json:"id"`
}

type eapiParam struct {
	Version int      `json:"version"`
	Cmds    []string `json:"cmds"`
	Format  string   `json:"format"`
}

type eapiResp struct {
	Result []struct {
		Interfaces map[string]eapiIface `json:"interfaces"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type eapiIface struct {
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	InterfaceStatus    string  `json:"interfaceStatus"`
	LineProtocolStatus string  `json:"lineProtocolStatus"`
	Bandwidth          float64 `json:"bandwidth"`
	InterfaceCounters  struct {
		InOctets       float64 `json:"inOctets"`
		OutOctets      float64 `json:"outOctets"`
		TotalInErrors  float64 `json:"totalInErrors"`
		TotalOutErrors float64 `json:"totalOutErrors"`
		InDiscards     float64 `json:"inDiscards"`
		OutDiscards    float64 `json:"outDiscards"`
	} `json:"interfaceCounters"`
	LastStatusChangeTimestamp float64 `json:"lastStatusChangeTimestamp"`
}

// fetchInterfaces runs `show interfaces` on a device over eAPI and converts the
// physical/management ports into collector.PortSample values.
func fetchInterfaces(ctx context.Context, hc *http.Client, ip, user, pass string) ([]collector.PortSample, error) {
	reqBody, _ := json.Marshal(eapiReq{
		JSONRPC: "2.0",
		Method:  "runCmds",
		Params:  eapiParam{Version: 1, Cmds: []string{"show interfaces"}, Format: "json"},
		ID:      "dvc-cvp",
	})
	url := fmt.Sprintf("https://%s/command-api", ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("eapi %s: status %d: %s", ip, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var er eapiResp
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	if er.Error != nil {
		return nil, fmt.Errorf("eapi %s: %s", ip, er.Error.Message)
	}
	if len(er.Result) == 0 {
		return nil, fmt.Errorf("eapi %s: empty result", ip)
	}

	now := time.Now()
	ports := make([]collector.PortSample, 0, len(er.Result[0].Interfaces))
	for name, ifc := range er.Result[0].Interfaces {
		if !isPort(name) {
			continue
		}
		admin, oper := mapStatus(ifc.InterfaceStatus)
		ports = append(ports, collector.PortSample{
			Name:        name,
			Description: ifc.Description,
			AdminStatus: admin,
			OperStatus:  oper,
			SpeedBps:    int64(ifc.Bandwidth),
			LastChange:  time.Unix(int64(ifc.LastStatusChangeTimestamp), 0),
			Counters: model.Counters{
				InOctets:    uint64(ifc.InterfaceCounters.InOctets),
				OutOctets:   uint64(ifc.InterfaceCounters.OutOctets),
				InErrors:    uint64(ifc.InterfaceCounters.TotalInErrors),
				OutErrors:   uint64(ifc.InterfaceCounters.TotalOutErrors),
				InDiscards:  uint64(ifc.InterfaceCounters.InDiscards),
				OutDiscards: uint64(ifc.InterfaceCounters.OutDiscards),
				Timestamp:   now,
			},
		})
	}
	return ports, nil
}

// isPort keeps physical Ethernet, management and port-channel interfaces and
// drops logical interfaces (Vlan, Loopback, Tunnel, …) that have no link.
func isPort(name string) bool {
	switch {
	case strings.HasPrefix(name, "Ethernet"),
		strings.HasPrefix(name, "Management"),
		strings.HasPrefix(name, "Port-Channel"):
		return true
	}
	return false
}

// mapStatus translates EOS interfaceStatus into our admin/oper model.
func mapStatus(s string) (admin, oper string) {
	switch s {
	case "connected":
		return "up", "connected"
	case "notconnect":
		return "up", "notconnect"
	case "disabled":
		return "down", "disabled"
	case "errdisabled":
		return "up", "errdisabled"
	default:
		return "up", s
	}
}
