// Package model defines the core domain types shared across the collector,
// store and API layers: data centers, Arista devices and their interfaces.
package model

import "time"

// Health status values used for data centers and devices.
const (
	StatusOK          = "ok"          // reachable, streaming/active
	StatusDegraded    = "degraded"    // partially reachable or stale telemetry
	StatusUnreachable = "unreachable" // proxy/CVP not reachable
	StatusInactive    = "inactive"    // device known but not streaming
)

// Port (interface) states. A port is "used" when it carries a link, "free"
// when administratively up but not connected (available for use), "disabled"
// when administratively shut down, and "error" when it has error/discard
// problems or is err-disabled.
const (
	PortUsed     = "used"
	PortFree     = "free"
	PortDisabled = "disabled"
	PortError    = "error"
)

// Device families we manage.
const (
	Family7280 = "7280"
	Family7504 = "7500"
	Family7010 = "7010"
)

// DataCenter is a monitored site. Each site is reached through its own proxy
// (typically an SSH jump host) and has its own CloudVision Portal endpoint.
type DataCenter struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Region      string    `json:"region"`
	City        string    `json:"city"`
	ProxyType   string    `json:"proxyType"`
	Status      string    `json:"status"`
	DeviceCount int       `json:"deviceCount"`
	DeviceUp    int       `json:"deviceUp"`
	PortTotal   int       `json:"portTotal"`
	PortUsed    int       `json:"portUsed"`
	PortFree    int       `json:"portFree"`
	InBps       float64   `json:"inBps"`
	OutBps      float64   `json:"outBps"`
	MaxUtilPct  float64   `json:"maxUtilPct"`
	LastPoll    time.Time `json:"lastPoll"`
	Error       string    `json:"error,omitempty"`
}

// Device is an Arista switch managed via CloudVision.
type Device struct {
	Serial     string    `json:"serial"`
	Hostname   string    `json:"hostname"`
	Model      string    `json:"model"`
	Family     string    `json:"family"`
	Version    string    `json:"version"`
	MgmtIP     string    `json:"mgmtIp"`
	DataCenter string    `json:"dataCenter"`
	Role       string    `json:"role"` // spine / leaf / mgmt
	Status     string    `json:"status"`
	UptimeSec  int64     `json:"uptimeSec"`
	PortTotal  int       `json:"portTotal"`
	PortUp     int       `json:"portUp"`
	PortUsed   int       `json:"portUsed"`
	PortFree   int       `json:"portFree"`
	PortErr    int       `json:"portErr"`
	InBps      float64   `json:"inBps"`
	OutBps     float64   `json:"outBps"`
	MaxUtilPct float64   `json:"maxUtilPct"`
	LastPoll   time.Time `json:"lastPoll"`
}

// Interface is a single physical/logical port on a device with its current
// computed rates and utilization.
type Interface struct {
	Device      string    `json:"device"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	AdminStatus string    `json:"adminStatus"`
	OperStatus  string    `json:"operStatus"`
	State       string    `json:"state"`
	SpeedBps    int64     `json:"speedBps"`
	InBps       float64   `json:"inBps"`
	OutBps      float64   `json:"outBps"`
	InUtilPct   float64   `json:"inUtilPct"`
	OutUtilPct  float64   `json:"outUtilPct"`
	UtilPct     float64   `json:"utilPct"`
	InErrors    int64     `json:"inErrors"`
	OutErrors   int64     `json:"outErrors"`
	InDiscards  int64     `json:"inDiscards"`
	OutDiscards int64     `json:"outDiscards"`
	LastChange  time.Time `json:"lastChange"`
	Neighbor    string    `json:"neighbor,omitempty"` // LLDP neighbor
}

// Counters holds raw interface counters sampled at a point in time. Rates are
// derived from the delta between two consecutive samples.
type Counters struct {
	InOctets    uint64
	OutOctets   uint64
	InErrors    uint64
	OutErrors   uint64
	InDiscards  uint64
	OutDiscards uint64
	Timestamp   time.Time
}

// Sample is one point in a time series of throughput for charting.
type Sample struct {
	T      time.Time `json:"t"`
	InBps  float64   `json:"inBps"`
	OutBps float64   `json:"outBps"`
}

// Summary is the global, top-level overview returned to the dashboard.
type Summary struct {
	DataCenters   int           `json:"dataCenters"`
	DataCentersOK int           `json:"dataCentersOk"`
	Devices       int           `json:"devices"`
	DevicesUp     int           `json:"devicesUp"`
	PortTotal     int           `json:"portTotal"`
	PortUsed      int           `json:"portUsed"`
	PortFree      int           `json:"portFree"`
	PortDisabled  int           `json:"portDisabled"`
	PortError     int           `json:"portError"`
	TotalInBps    float64       `json:"totalInBps"`
	TotalOutBps   float64       `json:"totalOutBps"`
	ByFamily      []FamilyCount `json:"byFamily"`
	UpdatedAt     time.Time     `json:"updatedAt"`
}

// FamilyCount counts devices per Arista family for the overview.
type FamilyCount struct {
	Family string `json:"family"`
	Count  int    `json:"count"`
}
