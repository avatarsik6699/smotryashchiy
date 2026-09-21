package domain

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

var now = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func batchWith(m Metric) Batch { return Batch{SchemaVersion: "1.0", Metrics: []Metric{m}} }

func okMetric() Metric {
	return Metric{Name: "cpu.usage_percent", TS: now, Value: 42, Labels: map[string]string{"core": "0"}}
}

func wantInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var apiErr *apierror.Error
	if !errors.As(err, &apiErr) || apiErr.Kind != apierror.KindInvalid {
		t.Fatalf("err = %v, want apierror.Invalid", err)
	}
	if !strings.Contains(err.Error(), field) {
		t.Fatalf("err %q does not address %q", err, field)
	}
}

// Lesson from sre-kit change 34: an idle container reports 0, which must never be dropped.
func TestZeroMetricValueIsValidAndPreserved(t *testing.T) {
	m := okMetric()
	m.Value = 0
	out, err := batchWith(m).Normalize(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 0 {
		t.Fatalf("zero metric lost: %+v", out.Metrics)
	}
}

func TestDecodeDistinguishesZeroFromMissingValue(t *testing.T) {
	zero := `{"schema_version":"1.0","metrics":[{"name":"a.b","ts":"2026-09-19T12:00:00Z","value":0}]}`
	b, err := DecodeBatch([]byte(zero))
	if err != nil || len(b.Metrics) != 1 || b.Metrics[0].Value != 0 {
		t.Fatalf("zero: %+v %v", b, err)
	}
	missing := `{"schema_version":"1.0","metrics":[{"name":"a.b","ts":"2026-09-19T12:00:00Z"}]}`
	if _, err := DecodeBatch([]byte(missing)); err == nil {
		t.Fatal("missing value must be rejected")
	} else {
		wantInvalid(t, err, "metrics[0].value")
	}
}

func TestDecodeRejectsMalformedAndIgnoresUnknownFields(t *testing.T) {
	if _, err := DecodeBatch([]byte("{")); err == nil {
		t.Fatal("expected error")
	}
	extra := `{"schema_version":"1.1","future":true,"events":[{"ts":"2026-09-19T12:00:00Z","level":"info","message":"m","novel":1}]}`
	b, err := DecodeBatch([]byte(extra))
	if err != nil || len(b.Events) != 1 {
		t.Fatalf("additive fields must be tolerated: %v", err)
	}
	if _, err := b.Normalize(now); err != nil {
		t.Fatal(err)
	}
}

// Lesson from sre-kit change 32: producer timestamps are validated.
func TestFutureTimestampBoundary(t *testing.T) {
	m := okMetric()
	m.TS = now.Add(MaxFutureSkew)
	if _, err := batchWith(m).Normalize(now); err != nil {
		t.Fatalf("exactly 5 minutes ahead must be accepted: %v", err)
	}
	m.TS = now.Add(MaxFutureSkew + time.Second)
	_, err := batchWith(m).Normalize(now)
	wantInvalid(t, err, "metrics[0].ts")
}

func TestMissingTimestampRejected(t *testing.T) {
	m := okMetric()
	m.TS = time.Time{}
	_, err := batchWith(m).Normalize(now)
	wantInvalid(t, err, "metrics[0].ts")
}

func TestTimestampsNormalizedToUTCMilliseconds(t *testing.T) {
	m := okMetric()
	m.TS = time.Date(2026, 9, 19, 15, 0, 0, 123456789, time.FixedZone("x", 3*3600))
	out, err := batchWith(m).Normalize(now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 19, 12, 0, 0, 123000000, time.UTC)
	if !out.Metrics[0].TS.Equal(want) || out.Metrics[0].TS.Location() != time.UTC {
		t.Fatalf("ts = %v, want %v", out.Metrics[0].TS, want)
	}
}

func TestMetricValidation(t *testing.T) {
	tests := map[string]func(*Metric){
		"name uppercase": func(m *Metric) { m.Name = "CPU" },
		"name empty":     func(m *Metric) { m.Name = "" },
		"name too long":  func(m *Metric) { m.Name = "a" + strings.Repeat("b", 128) },
		"NaN":            func(m *Metric) { m.Value = math.NaN() },
		"+Inf":           func(m *Metric) { m.Value = math.Inf(1) },
		"-Inf":           func(m *Metric) { m.Value = math.Inf(-1) },
		"label key":      func(m *Metric) { m.Labels = map[string]string{"Bad-Key": "v"} },
		"label value":    func(m *Metric) { m.Labels = map[string]string{"k": strings.Repeat("v", MaxLabelValue+1)} },
		"label count": func(m *Metric) {
			m.Labels = map[string]string{}
			for i := 0; i <= MaxLabels; i++ {
				m.Labels["k"+strings.Repeat("a", i)] = "v"
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := okMetric()
			mutate(&m)
			_, err := batchWith(m).Normalize(now)
			wantInvalid(t, err, "metrics[0]")
		})
	}
}

func TestCheckValidationAndMeta(t *testing.T) {
	good := Batch{SchemaVersion: "1.0", Checks: []Check{{Name: "disk.root", TS: now, Status: StatusOK}}}
	out, err := good.Normalize(now)
	if err != nil || string(out.Checks[0].Meta) != "{}" {
		t.Fatalf("absent meta must become {}: %v %s", err, out.Checks)
	}
	bad := map[string]Check{
		"status":      {Name: "a", TS: now, Status: "fine"},
		"meta array":  {Name: "a", TS: now, Status: StatusOK, Meta: json.RawMessage(`[1]`)},
		"meta string": {Name: "a", TS: now, Status: StatusOK, Meta: json.RawMessage(`"x"`)},
		"meta big":    {Name: "a", TS: now, Status: StatusOK, Meta: json.RawMessage(`{"k":"` + strings.Repeat("x", MaxMeta) + `"}`)},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := Batch{SchemaVersion: "1.0", Checks: []Check{c}}.Normalize(now)
			wantInvalid(t, err, "checks[0]")
		})
	}
}

func TestEventValidation(t *testing.T) {
	bad := map[string]Event{
		"level":   {TS: now, Level: "debug", Message: "m"},
		"empty":   {TS: now, Level: LevelInfo, Message: ""},
		"long":    {TS: now, Level: LevelInfo, Message: strings.Repeat("x", MaxMessage+1)},
		"utf8":    {TS: now, Level: LevelInfo, Message: "\xff"},
		"future":  {TS: now.Add(time.Hour), Level: LevelInfo, Message: "m"},
		"missing": {Level: LevelInfo, Message: "m"},
	}
	for name, e := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := Batch{SchemaVersion: "1.0", Events: []Event{e}}.Normalize(now)
			wantInvalid(t, err, "events[0]")
		})
	}
}

func TestBatchLevelRules(t *testing.T) {
	for _, v := range []string{"", "2.0", "1", "1.x", "1.00000"} {
		_, err := Batch{SchemaVersion: v}.Normalize(now)
		wantInvalid(t, err, "schema_version")
	}
	if _, err := (Batch{SchemaVersion: "1.3"}).Normalize(now); err != nil {
		t.Fatalf("empty batch (heartbeat) must be valid: %v", err)
	}
	big := Batch{SchemaVersion: "1.0"}
	for i := 0; i <= MaxRecords; i++ {
		big.Metrics = append(big.Metrics, okMetric())
	}
	_, err := big.Normalize(now)
	wantInvalid(t, err, "batch")
}

func TestOneBadRecordRejectsTheWholeBatch(t *testing.T) {
	b := Batch{SchemaVersion: "1.0", Metrics: []Metric{okMetric(), {Name: "BAD", TS: now}}}
	out, err := b.Normalize(now)
	wantInvalid(t, err, "metrics[1].name")
	if out.Len() != 0 {
		t.Fatal("no partial result may be returned")
	}
}

func TestCanonicalLabelsAreOrderIndependent(t *testing.T) {
	a := CanonicalLabels(map[string]string{"b": "2", "a": "1"})
	b := CanonicalLabels(map[string]string{"a": "1", "b": "2"})
	if a != b || a != `{"a":"1","b":"2"}` {
		t.Fatalf("%s vs %s", a, b)
	}
	if CanonicalLabels(nil) != "{}" || CanonicalLabels(map[string]string{}) != "{}" {
		t.Fatal("empty labels must serialize as {}")
	}
	if got := ParseLabels(a); got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("round trip: %v", got)
	}
	if got := ParseLabels("not json"); got == nil || len(got) != 0 {
		t.Fatalf("malformed -> %v", got)
	}
}
