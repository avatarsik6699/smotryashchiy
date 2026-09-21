package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// Contract limits (docs/SPEC.md §4.1).
const (
	MaxRecords        = 1000
	MaxLabels         = 16
	MaxLabelValue     = 128
	MaxMessage        = 2048
	MaxMeta           = 4096
	MaxIdempotencyKey = 128
	// MaxFutureSkew is how far ahead of the server clock a producer timestamp may be.
	MaxFutureSkew = 5 * time.Minute
)

var (
	nameRe     = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,127}$`)
	labelKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	versionRe  = regexp.MustCompile(`^1\.[0-9]{1,4}$`)
)

// Normalize validates b against the contract and returns a normalized copy: timestamps in UTC at
// millisecond precision, nil labels/meta replaced by empty objects. now is the server clock.
// The first violation is returned as an apierror.Invalid addressed to the offending field, and
// callers must then store nothing from the batch.
func (b Batch) Normalize(now time.Time) (Batch, error) {
	if !versionRe.MatchString(b.SchemaVersion) {
		return Batch{}, invalid("schema_version", "must be a 1.x version")
	}
	if b.Len() > MaxRecords {
		return Batch{}, invalid("batch", "contains %d records, limit is %d", b.Len(), MaxRecords)
	}
	out := Batch{SchemaVersion: b.SchemaVersion}
	for i, m := range b.Metrics {
		field := fmt.Sprintf("metrics[%d]", i)
		ts, labels, err := common(field, m.Name, m.TS, m.Labels, now)
		if err != nil {
			return Batch{}, err
		}
		if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
			return Batch{}, invalid(field+".value", "must be a finite number")
		}
		out.Metrics = append(out.Metrics, Metric{Name: m.Name, TS: ts, Value: m.Value, Labels: labels})
	}
	for i, c := range b.Checks {
		field := fmt.Sprintf("checks[%d]", i)
		ts, _, err := common(field, c.Name, c.TS, nil, now)
		if err != nil {
			return Batch{}, err
		}
		switch c.Status {
		case StatusOK, StatusWarn, StatusCritical:
		default:
			return Batch{}, invalid(field+".status", "must be ok, warn or critical")
		}
		meta, err := normalizeMeta(field+".meta", c.Meta)
		if err != nil {
			return Batch{}, err
		}
		out.Checks = append(out.Checks, Check{Name: c.Name, TS: ts, Status: c.Status, Meta: meta})
	}
	for i, e := range b.Events {
		field := fmt.Sprintf("events[%d]", i)
		ts, err := normalizeTS(field+".ts", e.TS, now)
		if err != nil {
			return Batch{}, err
		}
		switch e.Level {
		case LevelInfo, LevelWarn, LevelError, LevelCritical:
		default:
			return Batch{}, invalid(field+".level", "must be info, warn, error or critical")
		}
		if e.Message == "" || len(e.Message) > MaxMessage || !utf8.ValidString(e.Message) {
			return Batch{}, invalid(field+".message", "must be non-empty valid UTF-8 of at most %d bytes", MaxMessage)
		}
		labels, err := normalizeLabels(field+".labels", e.Labels)
		if err != nil {
			return Batch{}, err
		}
		out.Events = append(out.Events, Event{TS: ts, Level: e.Level, Message: e.Message, Labels: labels})
	}
	return out, nil
}

func common(field, name string, ts time.Time, labels map[string]string, now time.Time) (time.Time, map[string]string, error) {
	if !nameRe.MatchString(name) {
		return time.Time{}, nil, invalid(field+".name", "must match %s", nameRe)
	}
	normalized, err := normalizeTS(field+".ts", ts, now)
	if err != nil {
		return time.Time{}, nil, err
	}
	l, err := normalizeLabels(field+".labels", labels)
	return normalized, l, err
}

func normalizeTS(field string, ts, now time.Time) (time.Time, error) {
	if ts.IsZero() {
		return time.Time{}, invalid(field, "is required")
	}
	if ts.After(now.Add(MaxFutureSkew)) {
		return time.Time{}, invalid(field, "is more than %s in the future", MaxFutureSkew)
	}
	return ts.UTC().Truncate(time.Millisecond), nil
}

func normalizeLabels(field string, labels map[string]string) (map[string]string, error) {
	if len(labels) > MaxLabels {
		return nil, invalid(field, "has %d labels, limit is %d", len(labels), MaxLabels)
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		if !labelKeyRe.MatchString(k) {
			return nil, invalid(field, "label key %q must match %s", k, labelKeyRe)
		}
		if len(v) > MaxLabelValue || !utf8.ValidString(v) {
			return nil, invalid(field, "label %q value must be valid UTF-8 of at most %d bytes", k, MaxLabelValue)
		}
		out[k] = v
	}
	return out, nil
}

func normalizeMeta(field string, meta json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(meta))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("{}"), nil
	}
	if len(trimmed) > MaxMeta {
		return nil, invalid(field, "exceeds %d bytes", MaxMeta)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &object); err != nil {
		return nil, invalid(field, "must be a JSON object")
	}
	canonical, err := json.Marshal(object) // stable key order
	if err != nil {
		return nil, invalid(field, "must be a JSON object")
	}
	return canonical, nil
}

func invalid(field, format string, args ...any) error {
	return apierror.Invalid(field + ": " + fmt.Sprintf(format, args...))
}
