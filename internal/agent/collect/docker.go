package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultDockerSocket is the standard Docker Engine socket path.
const DefaultDockerSocket = "/var/run/docker.sock"

// Docker reports per-container CPU and memory usage via the local Docker socket
// (docs/SPEC.md §4h), read-only. A missing or unreadable socket (no Docker installed, or the
// agent's user lacks access) surfaces as a Collect error, same as any other unreadable source —
// the collector disables itself for the tick rather than fabricating data.
type Docker struct {
	socketPath string
	client     *http.Client
}

// NewDocker builds a collector talking to socketPath ("" defaults to DefaultDockerSocket).
func NewDocker(socketPath string) *Docker {
	if socketPath == "" {
		socketPath = DefaultDockerSocket
	}
	return &Docker{
		socketPath: socketPath,
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

func (*Docker) Name() string { return "docker" }

type dockerContainer struct {
	ID    string   `json:"Id"`
	Names []string `json:"Names"`
	Image string   `json:"Image"`
}

type dockerCPUUsage struct {
	TotalUsage  uint64   `json:"total_usage"`
	PercpuUsage []uint64 `json:"percpu_usage"`
}

type dockerCPUStats struct {
	CPUUsage       dockerCPUUsage `json:"cpu_usage"`
	SystemCPUUsage uint64         `json:"system_cpu_usage"`
	OnlineCPUs     uint32         `json:"online_cpus"`
}

type dockerMemoryStats struct {
	Usage uint64 `json:"usage"`
	Limit uint64 `json:"limit"`
	Stats struct {
		Cache uint64 `json:"cache"`
	} `json:"stats"`
}

type dockerStats struct {
	CPUStats    dockerCPUStats    `json:"cpu_stats"`
	PreCPUStats dockerCPUStats    `json:"precpu_stats"`
	MemoryStats dockerMemoryStats `json:"memory_stats"`
}

// Collect lists running containers and reads each one's resource usage. A single container's
// stats failing (e.g. it exited between the list and the stats call) is skipped, not fatal to the
// whole tick; only an unreachable daemon fails the collector outright.
func (d *Docker) Collect() ([]Sample, error) {
	var containers []dockerContainer
	if err := d.get("/containers/json", &containers); err != nil {
		return nil, fmt.Errorf("docker: list containers: %w", err)
	}
	var samples []Sample
	for _, c := range containers {
		var stats dockerStats
		if err := d.get("/containers/"+c.ID+"/stats?stream=false", &stats); err != nil {
			continue
		}
		labels := map[string]string{"container": containerName(c), "image": c.Image}
		if pct, ok := cpuPercent(stats); ok {
			samples = append(samples, Sample{Name: "docker.container.cpu_percent", Value: pct, Labels: labels})
		}
		if stats.MemoryStats.Limit > 0 {
			used := stats.MemoryStats.Usage
			if stats.MemoryStats.Stats.Cache < used {
				used -= stats.MemoryStats.Stats.Cache // working set, not raw cgroup usage (which includes reclaimable page cache)
			}
			samples = append(samples,
				Sample{Name: "docker.container.memory_used_bytes", Value: float64(used), Labels: labels},
				Sample{Name: "docker.container.memory_used_percent", Value: float64(used) / float64(stats.MemoryStats.Limit) * 100, Labels: labels},
			)
		}
	}
	return samples, nil
}

func containerName(c dockerContainer) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID
}

// cpuPercent follows the same delta formula as `docker stats`: precpu_stats is the previous
// read, so a one-shot stream=false call still yields a valid delta. A zero/negative system delta
// (e.g. the very first read of a just-started container) means no percentage is measurable yet.
func cpuPercent(s dockerStats) (float64, bool) {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
	if sysDelta <= 0 || cpuDelta < 0 {
		return 0, false
	}
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpus == 0 {
		cpus = 1
	}
	return (cpuDelta / sysDelta) * cpus * 100, true
}

func (d *Docker) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker: %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
