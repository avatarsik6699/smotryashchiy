package collect

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func procTree(t *testing.T, files map[string]string) Proc {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Proc{Root: root}
}

func byName(samples []Sample) map[string]float64 {
	m := map[string]float64{}
	for _, s := range samples {
		m[s.Name] = s.Value
	}
	return m
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCPUFirstTickMeasuresNothingThenReportsDelta(t *testing.T) {
	proc := procTree(t, map[string]string{"stat": "cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 50 0 50 350 50 0 0 0 0 0\n"})
	c := &CPU{Proc: proc}
	if s, err := c.Collect(); err != nil || len(s) != 0 {
		t.Fatalf("first tick = %v, %v; want no samples (no interval yet)", s, err)
	}
	// +100 user, +100 system, +200 idle, +0 iowait  => 200 busy of 400 => 50 %
	if err := os.WriteFile(filepath.Join(proc.Root, "stat"), []byte("cpu  200 0 200 900 100 0 0 0 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := c.Collect()
	if err != nil || len(s) != 1 || !near(s[0].Value, 50) || s[0].Name != "cpu.usage_percent" {
		t.Fatalf("second tick = %v, %v; want 50", s, err)
	}
}

func TestCPUFullyIdleIntervalIsAMeasuredZero(t *testing.T) {
	proc := procTree(t, map[string]string{"stat": "cpu  10 0 10 100 0 0 0 0 0 0\n"})
	c := &CPU{Proc: proc}
	_, _ = c.Collect()
	_ = os.WriteFile(filepath.Join(proc.Root, "stat"), []byte("cpu  10 0 10 200 0 0 0 0 0 0\n"), 0o644)
	s, err := c.Collect()
	if err != nil || len(s) != 1 || s[0].Value != 0 {
		t.Fatalf("idle interval = %v, %v; want a measured 0, not an omission", s, err)
	}
}

func TestCPUCounterResetAndUnreadableSourceEmitNothing(t *testing.T) {
	proc := procTree(t, map[string]string{"stat": "cpu  100 0 100 700 100 0 0 0 0 0\n"})
	c := &CPU{Proc: proc}
	_, _ = c.Collect()
	_ = os.WriteFile(filepath.Join(proc.Root, "stat"), []byte("cpu  1 0 1 5 0 0 0 0 0 0\n"), 0o644) // went backwards (reboot/reset)
	if s, err := c.Collect(); err != nil || len(s) != 0 {
		t.Fatalf("counter reset = %v, %v; want no samples", s, err)
	}
	if s, err := (&CPU{Proc: Proc{Root: t.TempDir()}}).Collect(); err == nil || len(s) != 0 {
		t.Fatalf("missing /proc/stat = %v, %v; want an error and no fabricated zero", s, err)
	}
}

const meminfo = `MemTotal:        8000000 kB
MemFree:          500000 kB
MemAvailable:    2000000 kB
SwapTotal:       1000000 kB
SwapFree:         750000 kB
`

func TestMemoryAndSwap(t *testing.T) {
	s, err := Memory{Proc: procTree(t, map[string]string{"meminfo": meminfo})}.Collect()
	if err != nil {
		t.Fatal(err)
	}
	m := byName(s)
	kb := 1024.0
	if m["memory.total_bytes"] != 8000000*kb || m["memory.used_bytes"] != 6000000*kb || !near(m["memory.used_percent"], 75) {
		t.Fatalf("memory = %v", m)
	}
	if m["swap.total_bytes"] != 1000000*kb || m["swap.used_bytes"] != 250000*kb || !near(m["swap.used_percent"], 25) {
		t.Fatalf("swap = %v", m)
	}
}

func TestHostWithoutSwapReportsMeasuredZeros(t *testing.T) {
	body := "MemTotal: 1000 kB\nMemAvailable: 400 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n"
	s, err := Memory{Proc: procTree(t, map[string]string{"meminfo": body})}.Collect()
	m := byName(s)
	v, present := m["swap.total_bytes"]
	if err != nil || !present || v != 0 || m["swap.used_percent"] != 0 {
		t.Fatalf("no-swap host: %v, %v; want swap.total_bytes=0 and used_percent=0 present", m, err)
	}
}

func TestMemoryWithoutMemAvailableOmitsMemoryInsteadOfGuessing(t *testing.T) {
	body := "MemTotal: 1000 kB\nMemFree: 300 kB\n"
	if s, err := (Memory{Proc: procTree(t, map[string]string{"meminfo": body})}).Collect(); err == nil {
		t.Fatalf("got %v; MemAvailable is required, a guess from MemFree would be wrong", s)
	}
}

func TestLoadAndUptime(t *testing.T) {
	proc := procTree(t, map[string]string{"loadavg": "0.00 1.50 2.25 1/500 12345\n", "uptime": "3600.75 7000.00\n"})
	l, err := Load{Proc: proc}.Collect()
	lm := byName(l)
	if err != nil || lm["load.avg_1m"] != 0 || lm["load.avg_5m"] != 1.5 || lm["load.avg_15m"] != 2.25 {
		t.Fatalf("load = %v, %v", lm, err)
	}
	if _, present := lm["load.avg_1m"]; !present {
		t.Fatal("a load of 0.00 must be emitted")
	}
	u, err := Uptime{Proc: proc}.Collect()
	if err != nil || len(u) != 1 || u[0].Value != 3600.75 {
		t.Fatalf("uptime = %v, %v", u, err)
	}
}

func TestMalformedSourcesReturnErrors(t *testing.T) {
	proc := procTree(t, map[string]string{"loadavg": "garbage", "uptime": "", "net/dev": "Inter-|\n face |\n"})
	if _, err := (Load{Proc: proc}).Collect(); err == nil {
		t.Error("malformed loadavg must error")
	}
	if _, err := (Uptime{Proc: proc}).Collect(); err == nil {
		t.Error("empty uptime must error")
	}
	if _, err := (Network{Proc: proc}).Collect(); err == nil {
		t.Error("no interfaces must error")
	}
}

const mounts = `sysfs /sys sysfs rw 0 0
/dev/sda1 / ext4 rw 0 0
/dev/sda1 /var/lib/docker/bind ext4 rw 0 0
tmpfs /run tmpfs rw 0 0
overlay /var/lib/docker/overlay2/x/merged overlay rw 0 0
/dev/sdb1 /mnt/my\040disk xfs rw 0 0
`

func TestDiskUsesRealFilesystemsOncePerDeviceWithDfSemantics(t *testing.T) {
	stats := map[string]FSStat{
		"/":            {BlockSize: 4096, Blocks: 1000, Free: 400, Avail: 300}, // 100 blocks reserved for root
		"/mnt/my disk": {BlockSize: 1024, Blocks: 0},                           // empty pseudo stat is skipped
	}
	d := &Disk{Proc: procTree(t, map[string]string{"mounts": mounts}), Statfs: func(p string) (FSStat, error) { return stats[p], nil }}
	s, err := d.Collect()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 {
		t.Fatalf("samples = %v; want only / (bind mount, tmpfs, overlay and empty fs excluded)", s)
	}
	m := byName(s)
	if m["disk.total_bytes"] != 1000*4096 || m["disk.used_bytes"] != 600*4096 {
		t.Fatalf("bytes = %v", m)
	}
	// df: used / (used + avail) = 600 / 900
	if !near(m["disk.used_percent"], 100*600.0/900.0) {
		t.Fatalf("used_percent = %v, want %v (reserved blocks excluded)", m["disk.used_percent"], 100*600.0/900.0)
	}
	if s[0].Labels["mount"] != "/" || s[0].Labels["device"] != "/dev/sda1" {
		t.Fatalf("labels = %v", s[0].Labels)
	}
}

func TestDiskUnescapesMountPathsAndBoundsCount(t *testing.T) {
	got := parseMounts("/dev/sdb1 /mnt/my\\040disk xfs rw 0 0\n")
	if len(got) != 1 || got[0].point != "/mnt/my disk" {
		t.Fatalf("parseMounts = %+v", got)
	}
	var many string
	for i := 0; i < 40; i++ {
		many += "/dev/vd" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + " /m" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + " ext4 rw 0 0\n"
	}
	if n := len(parseMounts(many)); n != maxMounts {
		t.Fatalf("mounts = %d, want the cap %d", n, maxMounts)
	}
}

func TestDiskReportsErrorWhenEveryStatfsFails(t *testing.T) {
	d := &Disk{Proc: procTree(t, map[string]string{"mounts": "/dev/sda1 / ext4 rw 0 0\n"}), Statfs: func(string) (FSStat, error) { return FSStat{}, os.ErrPermission }}
	if s, err := d.Collect(); err == nil || len(s) != 0 {
		t.Fatalf("= %v, %v; want an error and no samples", s, err)
	}
}

const netdev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000 10 0 0 0 0 0 0 1000 10 0 0 0 0 0 0
  eth0: 5000 50 0 0 0 0 0 0 0 0 0 0 0 0 0 0
docker0: 0 0 0 0 0 0 0 0 200 2 0 0 0 0 0 0
`

func TestNetworkExcludesLoopbackAndKeepsMeasuredZeros(t *testing.T) {
	s, err := Network{Proc: procTree(t, map[string]string{"net/dev": netdev})}.Collect()
	if err != nil || len(s) != 4 {
		t.Fatalf("samples = %v, %v; want rx+tx for eth0 and docker0 only", s, err)
	}
	seen := map[string]float64{}
	for _, x := range s {
		seen[x.Name+"/"+x.Labels["interface"]] = x.Value
	}
	if seen["network.rx_bytes_total/eth0"] != 5000 || seen["network.tx_bytes_total/eth0"] != 0 || seen["network.rx_bytes_total/docker0"] != 0 || seen["network.tx_bytes_total/docker0"] != 200 {
		t.Fatalf("counters = %v", seen)
	}
	if _, ok := seen["network.rx_bytes_total/lo"]; ok {
		t.Fatal("loopback must be excluded")
	}
	if _, present := seen["network.tx_bytes_total/eth0"]; !present {
		t.Fatal("a zero tx counter must still be emitted")
	}
}

func TestRealProcIsReadableOnLinux(t *testing.T) {
	if _, err := os.Stat("/proc/stat"); err != nil {
		t.Skip("no /proc on this platform")
	}
	for _, c := range []Collector{&CPU{Proc: DefaultProc}, Memory{Proc: DefaultProc}, Load{Proc: DefaultProc}, Uptime{Proc: DefaultProc}, Network{Proc: DefaultProc}, NewDisk()} {
		if _, err := c.Collect(); err != nil {
			t.Errorf("%s on the real system: %v", c.Name(), err)
		}
	}
}

func TestCPUCountCountsPerCoreLinesOnEveryTick(t *testing.T) {
	proc := procTree(t, map[string]string{"stat": "cpu  1 0 1 9 0 0 0 0 0 0\ncpu0 1 0 1 5 0 0 0 0 0 0\ncpu1 0 0 0 4 0 0 0 0 0 0\ncpu10 0 0 0 1 0 0 0 0 0 0\nintr 12 0\nctxt 4\n"})
	c := CPUCount{Proc: proc}
	for tick := 0; tick < 2; tick++ { // not a delta: the first tick already reports it
		s, err := c.Collect()
		if err != nil || len(s) != 1 || s[0].Name != "cpu.count" || s[0].Value != 3 {
			t.Fatalf("tick %d = %v, %v; want cpu.count 3", tick, s, err)
		}
	}
	if s, err := (CPUCount{Proc: procTree(t, map[string]string{"stat": "cpu  1 0 1 9\nintr 1\n"})}).Collect(); err == nil || len(s) != 0 {
		t.Fatalf("no per-core lines = %v, %v; want an error, never a count of 0", s, err)
	}
}
