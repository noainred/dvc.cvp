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
	Result []json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ifacesResult decodes the `show interfaces` command output.
type ifacesResult struct {
	Interfaces map[string]eapiIface `json:"interfaces"`
}

// xcvrResult decodes the `show interfaces transceiver` command output. EOS
// field names vary slightly across releases, so parsing is best-effort.
type xcvrResult struct {
	Interfaces map[string]eapiXcvr `json:"interfaces"`
}

type eapiXcvr struct {
	MediaType   string  `json:"mediaType"`
	VendorName  string  `json:"vendorName"`
	VendorSn    string  `json:"vendorSn"`
	VendorPn    string  `json:"vendorPn"`
	Temperature float64 `json:"temperature"`
	Voltage     float64 `json:"voltage"`
	TxPower     float64 `json:"txPower"`
	RxPower     float64 `json:"rxPower"`
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
		Params:  eapiParam{Version: 1, Cmds: []string{"show interfaces", "show interfaces transceiver"}, Format: "json"},
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

	var ir ifacesResult
	if err := json.Unmarshal(er.Result[0], &ir); err != nil {
		return nil, fmt.Errorf("eapi %s: decode interfaces: %w", ip, err)
	}
	// Transceiver data is optional/best-effort (second command).
	xcvr := map[string]eapiXcvr{}
	if len(er.Result) > 1 {
		var xr xcvrResult
		if err := json.Unmarshal(er.Result[1], &xr); err == nil {
			xcvr = xr.Interfaces
		}
	}

	now := time.Now()
	ports := make([]collector.PortSample, 0, len(ir.Interfaces))
	for name, ifc := range ir.Interfaces {
		if !isPort(name) {
			continue
		}
		admin, oper := mapStatus(ifc.InterfaceStatus)
		p := collector.PortSample{
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
		}
		if x, ok := xcvr[name]; ok {
			applyXcvr(&p, x, oper)
		}
		ports = append(ports, p)
	}
	return ports, nil
}

// applyXcvr merges transceiver/DOM data into a port sample. Presence is
// inferred from a non-empty, non-copper media type; DOM is considered valid
// only for a connected optic reporting optical power.
func applyXcvr(p *collector.PortSample, x eapiXcvr, oper string) {
	p.MediaType = x.MediaType
	if x.MediaType == "" || strings.Contains(x.MediaType, "BASE-T") {
		return // copper / no pluggable optic
	}
	p.HasTransceiver = true
	p.XcvrVendor = x.VendorName
	p.XcvrPart = x.VendorPn
	p.XcvrSerial = x.VendorSn
	if oper == "connected" && (x.TxPower != 0 || x.RxPower != 0) {
		p.DomValid = true
		p.TxPowerDbm = x.TxPower
		p.RxPowerDbm = x.RxPower
		p.TempC = x.Temperature
		p.VoltageV = x.Voltage
	}
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
