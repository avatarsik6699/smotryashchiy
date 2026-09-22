package collect

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func dockerLogFrame(stderr bool, payload string) []byte {
	streamType := byte(1)
	if stderr {
		streamType = 2
	}
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

func TestDemuxDockerLogStreamSplitsFramesIntoLines(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(dockerLogFrame(false, "hello "))
	buf.Write(dockerLogFrame(false, "world\nsecond line\n"))
	buf.Write(dockerLogFrame(true, "an error\n"))

	type got struct {
		stderr bool
		line   string
	}
	var lines []got
	demuxDockerLogStream(&buf, func(stderr bool, line string) {
		lines = append(lines, got{stderr, line})
	})
	want := []got{{false, "hello world"}, {false, "second line"}, {true, "an error"}}
	if len(lines) != len(want) {
		t.Fatalf("got %+v, want %+v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line[%d] = %+v, want %+v", i, lines[i], want[i])
		}
	}
}

func fakeDockerdWithLogs(t *testing.T, container dockerContainer, logFrames []byte) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]dockerContainer{container})
	})
	mux.HandleFunc("/containers/"+container.ID+"/logs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(logFrames)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // keep the connection open like a real follow=true tail
	})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: mux}}
	srv.Start()
	t.Cleanup(srv.Close)
	return sock
}

func TestDockerLogsCollectTailsARunningContainer(t *testing.T) {
	container := dockerContainer{ID: "abc123", Names: []string{"/web"}, Image: "nginx:latest"}
	var frames bytes.Buffer
	frames.Write(dockerLogFrame(false, "server started\n"))
	frames.Write(dockerLogFrame(true, "warning: config deprecated\n"))
	sock := fakeDockerdWithLogs(t, container, frames.Bytes())

	d := NewDockerLogs(t.Context(), sock)
	var events []Event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := d.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		events = append(events, got...)
		if len(events) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	if events[0].Level != "info" || events[0].Message != "server started" || events[0].Labels["container"] != "web" {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].Level != "warn" || events[1].Message != "warning: config deprecated" {
		t.Fatalf("event[1] = %+v", events[1])
	}
}

func TestDockerLogsCollectFailsWhenDaemonIsUnreachable(t *testing.T) {
	d := NewDockerLogs(t.Context(), filepath.Join(t.TempDir(), "no-such.sock"))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := d.Collect(); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("want Collect to eventually report an error when the daemon is unreachable")
}
