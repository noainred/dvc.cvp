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
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Region        string    `json:"region"`
	City          string    `json:"city"`
	ProxyType     string    `json:"proxyType"`
	Status        string    `json:"status"`
	DeviceCount   int       `json:"deviceCount"`
	DeviceUp      int       `json:"deviceUp"`
	PortTotal     int       `json:"portTotal"`
	PortUsed      int       `json:"portUsed"`
	PortFree      int       `json:"portFree"`
	PortFreeReady int       `json:"portFreeReady"` // free ports with an optic installed
	InBps         float64   `json:"inBps"`
	OutBps        float64   `json:"outBps"`
	MaxUtilPct    float64   `json:"maxUtilPct"`
	OpticAlarms   int       `json:"opticAlarms"`
	LastPoll      time.Time `json:"lastPoll"`
	Error         string    `json:"error,omitempty"`
}

// Device is an Arista switch managed via CloudVision.
type Device struct {
	Serial        string    `json:"serial"`
	Hostname      string    `json:"hostname"`
	Model         string    `json:"model"`
	Family        string    `json:"family"`
	Version       string    `json:"version"`
	MgmtIP        string    `json:"mgmtIp"`
	DataCenter    string    `json:"dataCenter"`
	Role          string    `json:"role"` // spine / leaf / mgmt
	Status        string    `json:"status"`
	UptimeSec     int64     `json:"uptimeSec"`
	PortTotal     int       `json:"portTotal"`
	PortUp        int       `json:"portUp"`
	PortUsed      int       `json:"portUsed"`
	PortFree      int       `json:"portFree"`
	PortFreeReady int       `json:"portFreeReady"` // free ports with an optic installed
	PortErr       int       `json:"portErr"`
	OpticAlarms   int       `json:"opticAlarms"`
	InBps         float64   `json:"inBps"`
	OutBps        float64   `json:"outBps"`
	MaxUtilPct    float64   `json:"maxUtilPct"`
	LastPoll      time.Time `json:"lastPoll"`
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

	// Transceiver (optic / "GBIC") inventory. HasTransceiver distinguishes a
	// free port that already has an optic installed (ready to use) from a truly
	// empty port. MediaType is the optic/cable type (e.g. 100GBASE-SR4).
	HasTransceiver bool   `json:"hasTransceiver"`
	MediaType      string `json:"mediaType,omitempty"`
	XcvrVendor     string `json:"xcvrVendor,omitempty"`
	XcvrPart       string `json:"xcvrPart,omitempty"`
	XcvrSerial     string `json:"xcvrSerial,omitempty"`

	// DOM (Digital Optical Monitoring) readings for the installed optic.
	DomValid   bool    `json:"domValid"`
	TxPowerDbm float64 `json:"txPowerDbm,omitempty"`
	RxPowerDbm float64 `json:"rxPowerDbm,omitempty"`
	TempC      float64 `json:"tempC,omitempty"`
	VoltageV   float64 `json:"voltageV,omitempty"`
	OpticAlarm string  `json:"opticAlarm,omitempty"` // "", low-rx, low-tx, high-temp
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
	PortFreeReady int           `json:"portFreeReady"` // free ports with an optic installed
	PortFreeEmpty int           `json:"portFreeEmpty"` // free ports with no optic
	PortDisabled  int           `json:"portDisabled"`
	PortError     int           `json:"portError"`
	OpticAlarms   int           `json:"opticAlarms"`
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

// Alert severities.
const (
	SevCritical = "critical"
	SevWarning  = "warning"
)

// Alert is a threshold/health condition raised by the alert engine.
type Alert struct {
	Key        string    `json:"key"`
	Severity   string    `json:"severity"`
	Type       string    `json:"type"` // util | optic | errors | device | datacenter
	DataCenter string    `json:"dataCenter,omitempty"`
	Device     string    `json:"device,omitempty"`
	Hostname   string    `json:"hostname,omitempty"`
	Interface  string    `json:"interface,omitempty"`
	Message    string    `json:"message"`
	Value      float64   `json:"value,omitempty"`
	Since      time.Time `json:"since"`
	Active     bool      `json:"active"`
}
