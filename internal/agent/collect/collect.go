// Package collect reads host metrics from /proc and statfs (docs/SPEC.md §4c). Every collector
// omits what it cannot read: unknown must never be reported as a fabricated zero, while a measured
// zero is a real value and is emitted.
package collect

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Sample is one measured value of a metric series.
type Sample struct {
	Name   string
	Value  float64
	Labels map[string]string
}

// Collector produces the samples of one metric family for the current tick.
type Collector interface {
	// Name identifies the collector in logs.
	Name() string
	// Collect returns the samples measurable right now. An error means nothing was measured.
	Collect() ([]Sample, error)
}

// Check is one measured health state for the current tick (docs/SPEC.md §4.1).
type Check struct {
	Name   string
	Status string // ok|warn|critical
	Meta   map[string]any
}

// CheckCollector produces the checks of one family for the current tick.
type CheckCollector interface {
	Name() string
	// Collect returns the checks measurable right now. An error means nothing was measured.
	Collect() ([]Check, error)
}

// Event is one discrete occurrence forwarded to the server's events table (docs/SPEC.md §4.1). TS
// is optional: zero means "stamp it with the current tick" (a live snapshot, like a Check); a
// tailed source sets its own TS so several events buffered in one tick keep distinct timestamps —
// the server's event identity is (host, ts, level, message, labels), so same-tick stamping would
// silently collapse two genuinely different same-message events into one.
type Event struct {
	TS      time.Time
	Level   string // info|warn|error|critical
	Message string
	Labels  map[string]string
}

// EventCollector produces the events observed since its last Collect call. Unlike Collector and
// CheckCollector (a snapshot of current state), this is typically a continuous tailer that buffers
// internally between ticks; Collect drains that buffer. An error means the source is unavailable
// this tick (a transient tail gap), not that zero events occurred.
type EventCollector interface {
	Name() string
	Collect() ([]Event, error)
}

// Proc locates the procfs tree; tests point it at fixtures.
type Proc struct{ Root string }

// DefaultProc reads the real /proc.
var DefaultProc = Proc{Root: "/proc"}

func (p Proc) read(rel string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(p.Root, rel))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// CPU reports the non-idle share of all CPUs between two consecutive Collect calls, so the first
// call measures nothing and returns no samples.
type CPU struct {
	Proc      Proc
	prevTotal uint64
	prevIdle  uint64
	primed    bool
}

func (*CPU) Name() string { return "cpu" }

func (c *CPU) Collect() ([]Sample, error) {
	raw, err := c.Proc.read("stat")
	if err != nil {
		return nil, err
	}
	total, idle, err := parseCPULine(raw)
	if err != nil {
		return nil, err
	}
	prevTotal, prevIdle, primed := c.prevTotal, c.prevIdle, c.primed
	c.prevTotal, c.prevIdle, c.primed = total, idle, true
	if !primed || total <= prevTotal || idle < prevIdle {
		return nil, nil // no interval yet, or a counter reset: nothing measured
	}
	dTotal, dIdle := float64(total-prevTotal), float64(idle-prevIdle)
	return []Sample{{Name: "cpu.usage_percent", Value: clampPercent(100 * (1 - dIdle/dTotal))}}, nil
}

// CPUCount reports how many CPUs the host has (the `cpuN` lines of /proc/stat). Load average only
// means something relative to it (docs/SPEC.md §4c, Change 22).
type CPUCount struct{ Proc Proc }

func (CPUCount) Name() string { return "cpu_count" }

func (c CPUCount) Collect() ([]Sample, error) {
	raw, err := c.Proc.read("stat")
	if err != nil {
		return nil, err
	}
	n := 0
	for _, line := range strings.Split(raw, "\n") {
		if len(line) > 3 && strings.HasPrefix(line, "cpu") && line[3] >= '0' && line[3] <= '9' {
			n++
		}
	}
	if n == 0 {
		return nil, errors.New("collect: no per-CPU lines in /proc/stat")
	}
	return []Sample{{Name: "cpu.count", Value: float64(n)}}, nil
}

// parseCPULine returns the summed jiffies and the idle share (idle + iowait) of the aggregate cpu line.
func parseCPULine(stat string) (total, idle uint64, err error) {
	for _, line := range strings.Split(stat, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var vals []uint64
		// user nice system idle iowait irq softirq steal; guest columns are already inside user/nice.
		for i := 1; i < len(fields) && i <= 8; i++ {
			v, perr := strconv.ParseUint(fields[i], 10, 64)
			if perr != nil {
				return 0, 0, fmt.Errorf("collect: parse /proc/stat: %w", perr)
			}
			vals = append(vals, v)
			total += v
		}
		idle = vals[3]
		if len(vals) > 4 {
			idle += vals[4]
		}
		return total, idle, nil
	}
	return 0, 0, errors.New("collect: no aggregate cpu line in /proc/stat")
}

// Memory reports RAM and swap from /proc/meminfo; used = total - MemAvailable.
type Memory struct{ Proc Proc }

func (Memory) Name() string { return "memory" }

func (m Memory) Collect() ([]Sample, error) {
	raw, err := m.Proc.read("meminfo")
	if err != nil {
		return nil, err
	}
	info := parseMeminfo(raw)
	var out []Sample
	if total, ok := info["MemTotal"]; ok && total > 0 {
		if avail, ok := info["MemAvailable"]; ok && avail <= total {
			used := total - avail
			out = append(out,
				Sample{Name: "memory.total_bytes", Value: float64(total)},
				Sample{Name: "memory.used_bytes", Value: float64(used)},
				Sample{Name: "memory.used_percent", Value: percent(used, total)})
		}
	}
	// A host without swap has SwapTotal 0: that is a measurement (0 used), not an absence.
	if swapTotal, ok := info["SwapTotal"]; ok {
		if swapFree, ok := info["SwapFree"]; ok && swapFree <= swapTotal {
			used := swapTotal - swapFree
			out = append(out,
				Sample{Name: "swap.total_bytes", Value: float64(swapTotal)},
				Sample{Name: "swap.used_bytes", Value: float64(used)},
				Sample{Name: "swap.used_percent", Value: percent(used, swapTotal)})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("collect: /proc/meminfo has no usable memory fields")
	}
	return out, nil
}

// parseMeminfo returns each "Key:   value kB" line in bytes.
func parseMeminfo(raw string) map[string]uint64 {
	info := map[string]uint64{}
	for _, line := range strings.Split(raw, "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && fields[1] == "kB" {
			v *= 1024
		}
		info[key] = v
	}
	return info
}

// Load reports the 1, 5 and 15 minute load averages.
type Load struct{ Proc Proc }

func (Load) Name() string { return "load" }

func (l Load) Collect() ([]Sample, error) {
	raw, err := l.Proc.read("loadavg")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(raw)
	if len(fields) < 3 {
		return nil, errors.New("collect: malformed /proc/loadavg")
	}
	out := make([]Sample, 0, 3)
	for i, name := range []string{"load.avg_1m", "load.avg_5m", "load.avg_15m"} {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil, fmt.Errorf("collect: parse /proc/loadavg: %w", err)
		}
		out = append(out, Sample{Name: name, Value: v})
	}
	return out, nil
}

// Uptime reports seconds since boot.
type Uptime struct{ Proc Proc }

func (Uptime) Name() string { return "uptime" }

func (u Uptime) Collect() ([]Sample, error) {
	raw, err := u.Proc.read("uptime")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(raw)
	if len(fields) < 1 {
		return nil, errors.New("collect: malformed /proc/uptime")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, fmt.Errorf("collect: parse /proc/uptime: %w", err)
	}
	return []Sample{{Name: "uptime.seconds", Value: v}}, nil
}

func percent(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return clampPercent(100 * float64(part) / float64(total))
}

func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	}
	return v
}
