package collect

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// fakeDockerd serves the two endpoints Docker.Collect calls over a real unix socket, so the test
// exercises the actual transport, not just the JSON decoding.
func fakeDockerd(t *testing.T, containers []dockerContainer, statsByID map[string]dockerStats) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(containers)
	})
	for id, stats := range statsByID {
		mux.HandleFunc("/containers/"+id+"/stats", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(stats)
		})
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: mux}}
	srv.Start()
	t.Cleanup(srv.Close)
	return sock
}

func TestDockerCollectReportsCPUAndMemoryPerContainer(t *testing.T) {
	sock := fakeDockerd(t,
		[]dockerContainer{{ID: "abc123", Names: []string{"/web"}, Image: "nginx:latest"}},
		map[string]dockerStats{
			"abc123": {
				CPUStats: dockerCPUStats{
					CPUUsage:       dockerCPUUsage{TotalUsage: 2000000000, PercpuUsage: []uint64{1, 2}},
					SystemCPUUsage: 100000000000,
				},
				PreCPUStats: dockerCPUStats{
					CPUUsage:       dockerCPUUsage{TotalUsage: 1000000000},
					SystemCPUUsage: 90000000000,
				},
				MemoryStats: dockerMemoryStats{Usage: 50 * 1024 * 1024, Limit: 200 * 1024 * 1024},
			},
		},
	)
	d := NewDocker(sock)
	samples, err := d.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := byName(samples)
	if got["docker.container.cpu_percent"] <= 0 {
		t.Fatalf("cpu_percent = %v, want > 0", got["docker.container.cpu_percent"])
	}
	if got["docker.container.memory_used_bytes"] != 50*1024*1024 {
		t.Fatalf("memory_used_bytes = %v", got["docker.container.memory_used_bytes"])
	}
	if got["docker.container.memory_used_percent"] != 25 {
		t.Fatalf("memory_used_percent = %v, want 25", got["docker.container.memory_used_percent"])
	}
	for _, s := range samples {
		if s.Labels["container"] != "web" || s.Labels["image"] != "nginx:latest" {
			t.Fatalf("labels = %+v, want container=web image=nginx:latest", s.Labels)
		}
	}
}

func TestDockerCollectFailsWhenSocketIsUnreachable(t *testing.T) {
	d := NewDocker(filepath.Join(t.TempDir(), "no-such.sock"))
	if _, err := d.Collect(); err == nil {
		t.Fatal("want an error when the socket does not exist")
	}
}

// slowDockerd answers every stats call after delay, like a real stats?stream=false (which blocks
// ~2 s sampling CPU); a container in stuck never answers until the caller gives up.
func slowDockerd(t *testing.T, n int, delay time.Duration, stuck map[string]bool) string {
	t.Helper()
	containers := make([]dockerContainer, n)
	stats := dockerStats{MemoryStats: dockerMemoryStats{Usage: 10, Limit: 100}}
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	for i := range containers {
		id := fmt.Sprintf("c%d", i)
		containers[i] = dockerContainer{ID: id, Names: []string{"/" + id}, Image: "img"}
		mux.HandleFunc("/containers/"+id+"/stats", func(w http.ResponseWriter, r *http.Request) {
			if stuck[id] {
				<-r.Context().Done()
				return
			}
			time.Sleep(delay)
			_ = json.NewEncoder(w).Encode(stats)
		})
	}
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(containers)
	})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: mux}}
	srv.Start()
	t.Cleanup(srv.Close)
	return sock
}

// The infraege.ru case from the 2026-09-23 audit, scaled down: 9 containers whose stats each block,
// read one by one, outlasted the agent's interval (docs/SPEC.md §4h). Read concurrently they fit
// in the tick's deadline.
func TestDockerCollectReadsContainersConcurrentlyWithinTheDeadline(t *testing.T) {
	const delay = 400 * time.Millisecond // serial: 9 × 400 ms = 3.6 s
	d := NewDocker(slowDockerd(t, 9, delay, nil)).WithDeadline(2 * time.Second)
	if d.deadline != time.Second {
		t.Fatalf("deadline = %s, want interval − 1 s = 1 s", d.deadline)
	}
	started := time.Now()
	samples, err := d.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if took := time.Since(started); took > d.deadline+200*time.Millisecond {
		t.Fatalf("Collect took %s, want within the %s deadline", took, d.deadline)
	}
	if got := len(containersIn(samples)); got != 9 {
		t.Fatalf("got samples for %d containers, want all 9", got)
	}
}

func TestDockerCollectOmitsAContainerThatMissesTheDeadline(t *testing.T) {
	d := NewDocker(slowDockerd(t, 3, 10*time.Millisecond, map[string]bool{"c1": true})).WithDeadline(2 * time.Second)
	started := time.Now()
	samples, err := d.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if took := time.Since(started); took > d.deadline+200*time.Millisecond {
		t.Fatalf("a stuck container held the tick for %s", took)
	}
	got := containersIn(samples)
	if len(got) != 2 || got["c1"] {
		t.Fatalf("containers = %v, want c0 and c2 only (c1 omitted, not zeroed)", got)
	}
}

func TestDockerDeadlineFollowsTheInterval(t *testing.T) {
	for interval, want := range map[time.Duration]time.Duration{
		5 * time.Second:  4 * time.Second,
		10 * time.Second: 5 * time.Second,
		5 * time.Minute:  5 * time.Second,
	} {
		if got := NewDocker("").WithDeadline(interval).deadline; got != want {
			t.Errorf("interval %s: deadline %s, want %s", interval, got, want)
		}
	}
	if got := NewDocker("").deadline; got != DefaultDockerDeadline {
		t.Errorf("default deadline = %s, want %s", got, DefaultDockerDeadline)
	}
}

func containersIn(samples []Sample) map[string]bool {
	out := map[string]bool{}
	for _, s := range samples {
		out[s.Labels["container"]] = true
	}
	return out
}
