package collect

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// dockerLogsReconcile is how often the running-container list is re-fetched to start/stop tailers.
const dockerLogsReconcile = 15 * time.Second

// DockerLogs tails the stdout/stderr of every running container via the Docker socket and
// forwards new lines as events (docs/SPEC.md §4h). It reconciles the container list periodically:
// a newly-seen container gets a tailer goroutine, a gone one's tailer is stopped. It disables
// itself (Collect returns an error) only when the daemon itself is unreachable; one container's
// tail failing never stops the others.
type DockerLogs struct {
	ctx    context.Context
	client *http.Client

	mu      sync.Mutex
	buf     []Event
	tailers map[string]context.CancelFunc
	failed  error
}

// NewDockerLogs starts reconciling immediately; ctx bounds every tailer's lifetime. socketPath ""
// defaults to DefaultDockerSocket.
func NewDockerLogs(ctx context.Context, socketPath string) *DockerLogs {
	if socketPath == "" {
		socketPath = DefaultDockerSocket
	}
	d := &DockerLogs{
		ctx: ctx,
		client: &http.Client{ // no request Timeout: a log tail is meant to stay open indefinitely
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var nd net.Dialer
					return nd.DialContext(ctx, "unix", socketPath)
				},
			},
		},
		tailers: map[string]context.CancelFunc{},
	}
	go d.reconcileLoop(ctx)
	return d
}

func (*DockerLogs) Name() string { return "docker_logs" }

func (d *DockerLogs) reconcileLoop(ctx context.Context) {
	d.reconcile(ctx)
	ticker := time.NewTicker(dockerLogsReconcile)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reconcile(ctx)
		}
	}
}

func (d *DockerLogs) reconcile(ctx context.Context) {
	listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	var containers []dockerContainer
	err := d.get(listCtx, "/containers/json", &containers)
	cancel()

	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.failed = err
		return
	}
	d.failed = nil
	seen := make(map[string]bool, len(containers))
	for _, c := range containers {
		seen[c.ID] = true
		if _, active := d.tailers[c.ID]; active {
			continue
		}
		tctx, tcancel := context.WithCancel(ctx)
		d.tailers[c.ID] = tcancel
		go d.tail(tctx, c.ID, containerName(c))
	}
	for id, tcancel := range d.tailers {
		if !seen[id] {
			tcancel()
			delete(d.tailers, id)
		}
	}
}

func (d *DockerLogs) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
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

func (d *DockerLogs) tail(ctx context.Context, id, name string) {
	since := time.Now().Unix()
	url := fmt.Sprintf("http://docker/containers/%s/logs?follow=true&stdout=true&stderr=true&since=%d", id, since)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	demuxDockerLogStream(resp.Body, func(stderr bool, line string) {
		level := "info"
		if stderr {
			level = "warn"
		}
		d.mu.Lock()
		d.buf = append(d.buf, Event{Level: level, Message: truncateMessage(line), Labels: map[string]string{"container": name}})
		d.mu.Unlock()
	})
}

// demuxDockerLogStream reads Docker's multiplexed log stream — each frame is an 8-byte header
// (stream type, 3 zero bytes, big-endian uint32 payload size) followed by that many payload bytes
// — and calls emit once per complete line. Frame boundaries do not align with line boundaries, so
// an incomplete trailing line is buffered per stream (stdout/stderr) across reads.
func demuxDockerLogStream(r io.Reader, emit func(stderr bool, line string)) {
	var header [8]byte
	pending := map[bool]string{}
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return
		}
		stderr := header[0] == 2
		size := binary.BigEndian.Uint32(header[4:8])
		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			return
		}
		text := pending[stderr] + string(payload)
		lines := strings.Split(text, "\n")
		pending[stderr] = lines[len(lines)-1]
		for _, line := range lines[:len(lines)-1] {
			if line != "" {
				emit(stderr, line)
			}
		}
	}
}

// Collect drains whatever the tailer goroutines have buffered since the last call.
func (d *DockerLogs) Collect() ([]Event, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failed != nil {
		return nil, d.failed
	}
	if len(d.buf) == 0 {
		return nil, nil
	}
	out := d.buf
	d.buf = nil
	return out, nil
}
