package domain

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// The wire types use pointers so a missing required field is distinguishable from a zero value:
// `"value": 0` is valid, an absent `value` is not.
type wireBatch struct {
	SchemaVersion string       `json:"schema_version"`
	Metrics       []wireMetric `json:"metrics"`
	Checks        []wireCheck  `json:"checks"`
	Events        []wireEvent  `json:"events"`
}

type wireMetric struct {
	Name   string            `json:"name"`
	TS     *time.Time        `json:"ts"`
	Value  *float64          `json:"value"`
	Labels map[string]string `json:"labels"`
}

type wireCheck struct {
	Name   string          `json:"name"`
	TS     *time.Time      `json:"ts"`
	Status string          `json:"status"`
	Meta   json.RawMessage `json:"meta"`
}

type wireEvent struct {
	TS      *time.Time        `json:"ts"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Labels  map[string]string `json:"labels"`
}

// DecodeBatch parses a wire batch. Unknown fields are ignored so additive 1.x extensions from a
// newer producer do not break an older server. The result still needs Batch.Normalize.
func DecodeBatch(data []byte) (Batch, error) {
	var w wireBatch
	if err := json.Unmarshal(data, &w); err != nil {
		return Batch{}, apierror.Invalid("malformed batch: " + err.Error())
	}
	b := Batch{SchemaVersion: w.SchemaVersion}
	for i, m := range w.Metrics {
		if m.Value == nil {
			return Batch{}, invalid("metrics["+strconv.Itoa(i)+"].value", "is required")
		}
		b.Metrics = append(b.Metrics, Metric{Name: m.Name, TS: deref(m.TS), Value: *m.Value, Labels: m.Labels})
	}
	for _, c := range w.Checks {
		b.Checks = append(b.Checks, Check{Name: c.Name, TS: deref(c.TS), Status: c.Status, Meta: c.Meta})
	}
	for _, e := range w.Events {
		b.Events = append(b.Events, Event{TS: deref(e.TS), Level: e.Level, Message: e.Message, Labels: e.Labels})
	}
	return b, nil
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{} // Normalize reports the missing timestamp
	}
	return *t
}
