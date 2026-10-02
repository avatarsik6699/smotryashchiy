package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/agent/collect"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

func TestResourceWaitCollectorsPassServerValidation(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("stat", "cpu 100 0 100 700 100 0 0 0\n")
	cpu := &collect.CPU{Proc: collect.Proc{Root: root}}
	if _, err := cpu.Collect(); err != nil {
		t.Fatal(err)
	}
	write("stat", "cpu 120 0 120 740 110 0 0 10\n")
	write("pressure/cpu", "some avg10=2.00 avg60=1.00 avg300=0.00 total=123\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	write("pressure/memory", "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	write("pressure/io", "some avg10=25.00 avg60=15.00 avg300=5.00 total=5000\nfull avg10=10.00 avg60=5.00 avg300=1.00 total=1000\n")
	samples, err := cpu.Collect()
	if err != nil {
		t.Fatal(err)
	}
	pressure, err := (collect.Pressure{Proc: collect.Proc{Root: root}}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	raw, skipped, err := BuildBatch(now, append(samples, pressure...), nil, nil)
	if err != nil || skipped != 0 {
		t.Fatalf("batch = %s, skipped=%d, err=%v", raw, skipped, err)
	}
	batch, err := domain.DecodeBatch(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Normalize(now); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"cpu.usage_percent": 50, "cpu.iowait_percent": 10, "cpu.steal_percent": 10,
		"pressure.cpu.some_percent": 2, "pressure.memory.some_percent": 0,
		"pressure.memory.full_percent": 0, "pressure.io.some_percent": 25,
		"pressure.io.full_percent": 10,
	}
	if len(batch.Metrics) != len(want) {
		t.Fatalf("metrics = %+v", batch.Metrics)
	}
	for _, m := range batch.Metrics {
		value, ok := want[m.Name]
		if !ok || m.Value != value {
			t.Errorf("unexpected metric %+v", m)
		}
		delete(want, m.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing metrics: %v", want)
	}
}
