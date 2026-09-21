// Package domain holds the telemetry value types and their validation rules (docs/SPEC.md §4.1).
package domain

import (
	"encoding/json"
	"net/netip"
	"time"
)

// SchemaVersion is the wire contract version this build emits and stores.
const SchemaVersion = "1.0"

// Check statuses and event levels accepted by the contract.
const (
	StatusOK       = "ok"
	StatusWarn     = "warn"
	StatusCritical = "critical"

	LevelInfo     = "info"
	LevelWarn     = "warn"
	LevelError    = "error"
	LevelCritical = "critical"
)

// Metric is one time-series sample. Value 0 is a real, storable measurement.
type Metric struct {
	Name   string
	TS     time.Time
	Value  float64
	Labels map[string]string
}

// Check is a discrete status snapshot.
type Check struct {
	Name   string
	TS     time.Time
	Status string
	Meta   json.RawMessage // JSON object; "{}" when absent
}

// Event is a discrete occurrence.
type Event struct {
	TS      time.Time
	Level   string
	Message string
	Labels  map[string]string
}

// Batch is the unit a producer submits and the unit that is stored atomically.
type Batch struct {
	SchemaVersion string
	Metrics       []Metric
	Checks        []Check
	Events        []Event
}

// Len returns the total number of records in the batch.
func (b Batch) Len() int { return len(b.Metrics) + len(b.Checks) + len(b.Events) }

// Host is a machine identity that emits telemetry.
type Host struct {
	ID         string
	Name       string
	CreatedAt  time.Time
	LastSeenAt *time.Time // nil until the first stored batch
}

// MetricPoint, CheckState and EventEntry are read models: stored records with their host.
type MetricPoint struct {
	HostID string
	Metric
}

type CheckState struct {
	HostID string
	Check
}

type EventEntry struct {
	HostID string
	Event
}

// RollupPoint is one hourly aggregate of a metric series. TS is the start of the hour (UTC).
// A zero-only hour has Count > 0 and Min = Max = Avg = 0: zero is a value, absence is no point.
type RollupPoint struct {
	HostID string
	Name   string
	Labels map[string]string
	TS     time.Time
	Count  int64
	Min    float64
	Max    float64
	Avg    float64
}

// Peer binds an enrolled host to its WireGuard public key and tunnel address.
type Peer struct {
	HostID     string
	PublicKey  string // base64
	TunnelIP   netip.Addr
	EnrolledAt time.Time
}
