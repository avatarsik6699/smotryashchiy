// Package domain holds the uptime prober's value types and validation (docs/SPEC.md §4e).
package domain

import (
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// Kind selects how a target is probed.
type Kind string

const (
	KindHTTP Kind = "http"
	KindTCP  Kind = "tcp"
	KindTLS  Kind = "tls"
)

// Limits of the target definition.
const (
	MaxTargets         = 50
	MinIntervalSeconds = 30
	MaxIntervalSeconds = 3600
	DefaultInterval    = 60
	MaxNameBytes       = 80
	MaxURLBytes        = 2048
	CheckTimeout       = 10 * time.Second
)

// Target is an operator-defined thing to probe.
type Target struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Kind            Kind      `json:"kind"`
	Address         string    `json:"target"`
	IntervalSeconds int       `json:"interval_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}

// Result is one observation. Latency is nil for a failed check: "no measurement" is not "0 ms".
type Result struct {
	TargetID      string     `json:"target_id"`
	TS            time.Time  `json:"ts"`
	OK            bool       `json:"ok"`
	LatencyMS     *int64     `json:"latency_ms"`
	StatusCode    *int       `json:"status_code"`
	Error         string     `json:"error"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
}

// LatencyPoint is one history sample; Latency is nil for a failed check. It marshals as [ts_ms, latency|null].
type LatencyPoint struct {
	TS      int64
	Latency *int64
}

func (p LatencyPoint) MarshalJSON() ([]byte, error) { return json.Marshal([2]any{p.TS, p.Latency}) }

// NewTarget is a target definition as submitted, before validation.
type NewTarget struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Address         string `json:"target"`
	IntervalSeconds int    `json:"interval_seconds"`
}

// Validate normalizes the definition (trimmed name, default interval) or returns a field-addressed error.
func (n NewTarget) Validate() (NewTarget, error) {
	n.Name = strings.TrimSpace(n.Name)
	n.Address = strings.TrimSpace(n.Address)
	if n.Name == "" || len(n.Name) > MaxNameBytes {
		return NewTarget{}, apierror.Invalid("name must contain 1.." + strconv.Itoa(MaxNameBytes) + " bytes")
	}
	if n.IntervalSeconds == 0 {
		n.IntervalSeconds = DefaultInterval
	}
	if n.IntervalSeconds < MinIntervalSeconds || n.IntervalSeconds > MaxIntervalSeconds {
		return NewTarget{}, apierror.Invalid("interval_seconds must be between " + strconv.Itoa(MinIntervalSeconds) + " and " + strconv.Itoa(MaxIntervalSeconds))
	}
	switch Kind(n.Kind) {
	case KindHTTP:
		if err := validateURL(n.Address); err != nil {
			return NewTarget{}, err
		}
	case KindTCP, KindTLS:
		if err := validateHostPort(n.Address); err != nil {
			return NewTarget{}, err
		}
	default:
		return NewTarget{}, apierror.Invalid("kind must be http, tcp or tls")
	}
	return n, nil
}

func validateURL(raw string) error {
	if raw == "" || len(raw) > MaxURLBytes {
		return apierror.Invalid("target must be an http(s) URL of at most " + strconv.Itoa(MaxURLBytes) + " bytes")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return apierror.Invalid("target must be an absolute http(s) URL")
	}
	if u.User != nil {
		return apierror.Invalid("target must not contain credentials")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return apierror.Invalid("target has an invalid port")
		}
	}
	return nil
}

func validateHostPort(raw string) error {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" || len(host) > 253 {
		return apierror.Invalid("target must be host:port")
	}
	if strings.ContainsAny(host, " /\\@") {
		return apierror.Invalid("target host contains invalid characters")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return apierror.Invalid("target has an invalid port")
	}
	return nil
}
