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

type wireBatch struct {
	SchemaVersion string       `json:"schema_version"`
	Metrics       []wireMetric `json:"metrics"`
}

// schemaVersion is the wire contract version the agent speaks (docs/SPEC.md §4.1).
const schemaVersion = "1.0"

// BuildBatch turns the samples of one tick into a wire batch stamped ts (UTC). Non-finite values
// are dropped and counted rather than sent, because the server would reject the whole batch and a
// broken reading must not take healthy ones down with it. It returns nil when there is nothing to
// send.
func BuildBatch(ts time.Time, samples []collect.Sample) (batch []byte, skipped int, err error) {
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
	if len(metrics) == 0 {
		return nil, skipped, nil
	}
	batch, err = json.Marshal(wireBatch{SchemaVersion: schemaVersion, Metrics: metrics})
	return batch, skipped, err
}
