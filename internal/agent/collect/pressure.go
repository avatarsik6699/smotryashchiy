package collect

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// Pressure reports Linux PSI avg10 values when the kernel exposes them. Files are optional, and
// each valid source is retained independently when another source is absent or malformed.
type Pressure struct{ Proc Proc }

func (Pressure) Name() string { return "pressure" }

func (p Pressure) Collect() ([]Sample, error) {
	sources := []struct {
		file string
		name string
	}{
		{"pressure/cpu", "pressure.cpu"},
		{"pressure/memory", "pressure.memory"},
		{"pressure/io", "pressure.io"},
	}
	var out []Sample
	var sourceErr error
	for _, source := range sources {
		raw, err := p.Proc.read(source.file)
		if err != nil {
			if !os.IsNotExist(err) && sourceErr == nil {
				sourceErr = fmt.Errorf("collect: read /proc/%s: %w", source.file, err)
			}
			continue
		}
		samples := parsePressureFile(raw, source.name)
		if len(samples) == 0 && sourceErr == nil {
			sourceErr = fmt.Errorf("collect: /proc/%s has no valid avg10 pressure values", source.file)
		}
		out = append(out, samples...)
	}
	if len(out) == 0 && sourceErr != nil {
		return nil, sourceErr
	}
	return out, nil
}

func parsePressureFile(raw, prefix string) []Sample {
	var out []Sample
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || (fields[0] != "some" && fields[0] != "full") {
			continue
		}
		if fields[0] == "full" && prefix == "pressure.cpu" {
			continue // CPU full is undefined at system level, even if the kernel reports 0.
		}
		var avg10 string
		for _, field := range fields[1:] {
			key, value, ok := strings.Cut(field, "=")
			if ok && key == "avg10" {
				avg10 = value
				break
			}
		}
		value, err := strconv.ParseFloat(avg10, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
			continue
		}
		name := prefix + ".some_percent"
		if fields[0] == "full" {
			name = prefix + ".full_percent"
		}
		out = append(out, Sample{Name: name, Value: value})
	}
	return out
}
