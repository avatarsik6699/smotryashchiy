package agent

import (
	"encoding/json"
	"math"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/agent/collect"
)

type wireMetric struct {
	Name   string            `json:"name"`
	TS     string            `json:"ts"`
	Value  float64           `json:"value"` // never omitempty: zero is a value
	Labels map[string]string `json:"labels"`
}

type wireCheck struct {
	Name   string         `json:"name"`
	TS     string         `json:"ts"`
	Status string         `json:"status"`
	Meta   map[string]any `json:"meta"`
}

type wireEvent struct {
	TS      string            `json:"ts"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Labels  map[string]string `json:"labels"`
}

type wireBatch struct {
	SchemaVersion string       `json:"schema_version"`
	Metrics       []wireMetric `json:"metrics"`
	Checks        []wireCheck  `json:"checks,omitempty"`
	Events        []wireEvent  `json:"events,omitempty"`
}

// schemaVersion is the wire contract version the agent speaks (docs/SPEC.md §4.1).
const schemaVersion = "1.0"

// BuildBatch turns one tick's samples, checks and events into a wire batch stamped ts (UTC).
// Non-finite metric values are dropped and counted rather than sent, because the server would
// reject the whole batch and a broken reading must not take healthy ones down with it. It returns
// nil when there is nothing to send.
func BuildBatch(ts time.Time, samples []collect.Sample, checks []collect.Check, events []collect.Event) (batch []byte, skipped int, err error) {
	stamp := ts.UTC().Format(time.RFC3339Nano)
	metrics := make([]wireMetric, 0, len(samples))
	for _, s := range samples {
		if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
			skipped++
			continue
		}
		labels := s.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		metrics = append(metrics, wireMetric{Name: s.Name, TS: stamp, Value: s.Value, Labels: labels})
	}
	wireChecks := make([]wireCheck, 0, len(checks))
	for _, c := range checks {
		wireChecks = append(wireChecks, wireCheck{Name: c.Name, TS: stamp, Status: c.Status, Meta: c.Meta})
	}
	wireEvents := make([]wireEvent, 0, len(events))
	for _, e := range events {
		labels := e.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		eventStamp := stamp
		if !e.TS.IsZero() {
			eventStamp = e.TS.UTC().Format(time.RFC3339Nano)
		}
		wireEvents = append(wireEvents, wireEvent{TS: eventStamp, Level: e.Level, Message: e.Message, Labels: labels})
	}
	if len(metrics) == 0 && len(wireChecks) == 0 && len(wireEvents) == 0 {
		return nil, skipped, nil
	}
	batch, err = json.Marshal(wireBatch{SchemaVersion: schemaVersion, Metrics: metrics, Checks: wireChecks, Events: wireEvents})
	return batch, skipped, err
}
