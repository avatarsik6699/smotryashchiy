package collect

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
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
