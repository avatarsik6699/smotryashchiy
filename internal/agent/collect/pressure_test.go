package collect

import "testing"

func TestPressureCollectsValidValuesAndNeverCPUFull(t *testing.T) {
	proc := procTree(t, map[string]string{
		"pressure/cpu":    "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
		"pressure/memory": "some avg10=1.25 avg60=0.00 avg300=0.00 total=10\nfull avg10=0.25 avg60=0.00 avg300=0.00 total=2\n",
		"pressure/io":     "some avg10=2.50 avg60=0.00 avg300=0.00 total=20\nfull avg10=0.50 avg60=0.00 avg300=0.00 total=4\n",
	})
	s, err := (Pressure{Proc: proc}).Collect()
	m := byName(s)
	if err != nil || len(m) != 5 || m["pressure.cpu.some_percent"] != 0 || m["pressure.memory.some_percent"] != 1.25 || m["pressure.memory.full_percent"] != 0.25 || m["pressure.io.some_percent"] != 2.5 || m["pressure.io.full_percent"] != 0.5 {
		t.Fatalf("pressure = %v, %v", m, err)
	}
	if _, present := m["pressure.cpu.full_percent"]; present {
		t.Fatal("CPU full pressure is undefined and must never be emitted")
	}
}

func TestPressureKeepsGoodSourcesWhenOthersAreMissingOrMalformed(t *testing.T) {
	proc := procTree(t, map[string]string{
		"pressure/cpu": "some avg10=0.00 avg60=0.00\nfull avg10=4\n",
		"pressure/io":  "some avg10=NaN\nfull avg10=+Inf\n",
	})
	s, err := (Pressure{Proc: proc}).Collect()
	if err != nil || len(s) != 1 || s[0].Name != "pressure.cpu.some_percent" || s[0].Value != 0 {
		t.Fatalf("partial pressure = %v, %v; want valid CPU zero only", s, err)
	}
	missing, err := (Pressure{Proc: Proc{Root: t.TempDir()}}).Collect()
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing optional PSI = %v, %v; want no samples and no error", missing, err)
	}
}

func TestPressureOmitsMalformedNonFiniteAndOutOfRangeValues(t *testing.T) {
	proc := procTree(t, map[string]string{
		"pressure/memory": "some avg10=missing\nfull avg10=NaN\n",
		"pressure/io":     "some avg10=Inf\nfull avg10=100.01\n",
		"pressure/cpu":    "some avg10=-0.01\n",
	})
	if s, err := (Pressure{Proc: proc}).Collect(); err == nil || len(s) != 0 {
		t.Fatalf("invalid pressure values = %v, %v; want no samples and an error", s, err)
	}
}
