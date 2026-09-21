package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/agent/collect"
	"github.com/avatarsik6699/smotryashchiy/internal/agent/spool"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestBuildBatchKeepsZerosAndPassesServerValidation(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	raw, skipped, err := BuildBatch(now, []collect.Sample{
		{Name: "cpu.usage_percent", Value: 0},
		{Name: "disk.used_percent", Value: 41.5, Labels: map[string]string{"mount": "/", "device": "/dev/sda1"}},
		{Name: "load.avg_1m", Value: math.NaN()},
		{Name: "load.avg_5m", Value: math.Inf(1)},
	})
	if err != nil || skipped != 2 {
		t.Fatalf("err = %v, skipped = %d; want the two non-finite samples skipped", err, skipped)
	}
	if !strings.Contains(string(raw), `"value":0`) {
		t.Fatalf("a measured zero must be serialized: %s", raw)
	}
	// The agent's output must be accepted verbatim by the server's own decoder and validator.
	batch, err := domain.DecodeBatch(raw)
	if err != nil {
		t.Fatalf("server decoder rejected the agent batch: %v\n%s", err, raw)
	}
	if _, err := batch.Normalize(now); err != nil {
		t.Fatalf("server validation rejected the agent batch: %v", err)
	}
	if len(batch.Metrics) != 2 || batch.Metrics[0].Value != 0 {
		t.Fatalf("decoded metrics = %+v", batch.Metrics)
	}
}

func TestBuildBatchWithNothingToSendReturnsNil(t *testing.T) {
	if raw, _, err := BuildBatch(time.Now(), nil); raw != nil || err != nil {
		t.Fatalf("= %s, %v; an empty tick must not produce a batch", raw, err)
	}
	if raw, skipped, _ := BuildBatch(time.Now(), []collect.Sample{{Name: "a.b", Value: math.NaN()}}); raw != nil || skipped != 1 {
		t.Fatalf("all-invalid tick = %s, %d", raw, skipped)
	}
}

func TestEveryCollectorMetricNameIsValidForTheServer(t *testing.T) {
	samples := []collect.Sample{
		{Name: "cpu.usage_percent"}, {Name: "memory.total_bytes"}, {Name: "memory.used_bytes"}, {Name: "memory.used_percent"},
		{Name: "swap.total_bytes"}, {Name: "swap.used_bytes"}, {Name: "swap.used_percent"},
		{Name: "disk.total_bytes", Labels: map[string]string{"mount": "/mnt/x y", "device": "/dev/sda1"}},
		{Name: "network.rx_bytes_total", Labels: map[string]string{"interface": "eth0"}},
		{Name: "load.avg_1m"}, {Name: "load.avg_5m"}, {Name: "load.avg_15m"}, {Name: "uptime.seconds"},
	}
	now := time.Now()
	raw, _, _ := BuildBatch(now, samples)
	batch, err := domain.DecodeBatch(raw)
	if err == nil {
		_, err = batch.Normalize(now)
	}
	if err != nil {
		t.Fatalf("catalog names/labels violate the contract: %v", err)
	}
}

func TestBackoffDoublesCapsAndJitters(t *testing.T) {
	for n, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 6: 32 * time.Second, 7: time.Minute, 30: time.Minute} {
		if got := Backoff(n, 0.5); got != want {
			t.Errorf("Backoff(%d, 0.5) = %v, want %v", n, got, want)
		}
	}
	if lo, hi := Backoff(3, 0), Backoff(3, 0.999999); lo != 3200*time.Millisecond || hi < 4700*time.Millisecond || hi > 4800*time.Millisecond {
		t.Errorf("jitter range = %v..%v, want ±20%% around 4s", lo, hi)
	}
	if Backoff(0, 0.5) != time.Second {
		t.Error("n < 1 must behave like n = 1")
	}
}

type fakeUplink struct {
	send   func(key string, batch []byte) (Result, error)
	closed *int
}

func (f fakeUplink) Send(_ context.Context, key string, batch []byte) (Result, error) {
	return f.send(key, batch)
}
func (f fakeUplink) Close() { *f.closed++ }

type harness struct {
	t      *testing.T
	sp     *spool.Spool
	sender *Sender
	sent   []string
	dials  int
	closed int
	answer func(key string) error
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	sp, err := spool.Open(filepath.Join(t.TempDir(), "spool"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, sp: sp, answer: func(string) error { return nil }}
	h.sender = NewSender(sp, func(context.Context) (Uplink, error) {
		h.dials++
		return fakeUplink{closed: &h.closed, send: func(key string, _ []byte) (Result, error) {
			if err := h.answer(key); err != nil {
				return Result{}, err
			}
			h.sent = append(h.sent, key)
			return Result{}, nil
		}}, nil
	}, quiet)
	return h
}

func (h *harness) add(keys ...string) {
	for _, k := range keys {
		if _, err := h.sp.Add(k, []byte(`{}`)); err != nil {
			h.t.Fatal(err)
		}
	}
}

func TestDrainSendsOldestFirstOverOneSessionAndEmptiesTheSpool(t *testing.T) {
	h := newHarness(t)
	h.add("a", "b", "c")
	if err := h.sender.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(h.sent, ",") != "a,b,c" || h.sp.Len() != 0 || h.dials != 1 {
		t.Fatalf("sent %v, left %d, dials %d; want a,b,c in order, empty spool, one session", h.sent, h.sp.Len(), h.dials)
	}
}

func TestTransientErrorsKeepTheBatchAndPreserveOrder(t *testing.T) {
	h := newHarness(t)
	h.add("a", "b")
	transient := []error{
		errors.New("network unreachable"),
		&StatusError{Code: http.StatusTooManyRequests, Body: "slow down"},
		&StatusError{Code: http.StatusInternalServerError, Body: "boom"},
		&StatusError{Code: http.StatusUnauthorized, Body: "unknown tunnel peer"},
	}
	for _, e := range transient {
		h.answer = func(string) error { return e }
		if err := h.sender.Drain(context.Background()); err == nil {
			t.Fatalf("%v must surface as a retryable failure", e)
		}
		if h.sp.Len() != 2 {
			t.Fatalf("%v: batch lost (left %d)", e, h.sp.Len())
		}
	}
	h.answer = func(string) error { return nil }
	if err := h.sender.Drain(context.Background()); err != nil || strings.Join(h.sent, ",") != "a,b" || h.sender.Failures() != 0 {
		t.Fatalf("after recovery: sent %v, err %v, failures %d", h.sent, err, h.sender.Failures())
	}
}

func TestPermanentRejectionIsDroppedWithoutBlockingTheQueue(t *testing.T) {
	h := newHarness(t)
	h.add("bad", "good1", "toobig", "good2")
	h.answer = func(key string) error {
		switch key {
		case "bad":
			return &StatusError{Code: http.StatusBadRequest, Body: "ts is too far in the future"}
		case "toobig":
			return &StatusError{Code: http.StatusRequestEntityTooLarge, Body: "batch exceeds 1 MiB"}
		}
		return nil
	}
	if err := h.sender.Drain(context.Background()); err != nil {
		t.Fatalf("drain = %v; permanent rejections must not surface as failures", err)
	}
	if strings.Join(h.sent, ",") != "good1,good2" || h.sp.Len() != 0 {
		t.Fatalf("sent %v, left %d", h.sent, h.sp.Len())
	}
}

func TestTunnelIsRebuiltAfterThreeConsecutiveFailures(t *testing.T) {
	h := newHarness(t)
	h.add("a")
	h.answer = func(string) error { return errors.New("no route") }
	for i := 0; i < 2; i++ {
		_ = h.sender.Drain(context.Background())
	}
	if h.dials != 1 || h.closed != 0 {
		t.Fatalf("after 2 failures: dials %d, closed %d; the tunnel must be kept", h.dials, h.closed)
	}
	_ = h.sender.Drain(context.Background()) // third failure
	if h.closed != 1 {
		t.Fatalf("closed = %d after 3 failures, want the session rebuilt", h.closed)
	}
	h.answer = func(string) error { return nil }
	if err := h.sender.Drain(context.Background()); err != nil || h.dials != 2 || h.sp.Len() != 0 {
		t.Fatalf("recovery: err %v, dials %d, left %d", err, h.dials, h.sp.Len())
	}
}

func TestDialFailureIsRetryableAndKeepsData(t *testing.T) {
	sp, _ := spool.Open(filepath.Join(t.TempDir(), "spool"), 0, 0)
	_, _ = sp.Add("a", []byte(`{}`))
	s := NewSender(sp, func(context.Context) (Uplink, error) { return nil, errors.New("no handshake") }, quiet)
	if err := s.Drain(context.Background()); err == nil || sp.Len() != 1 {
		t.Fatalf("err = %v, left %d; a failed dial must keep the batch", err, sp.Len())
	}
}

func TestValidateInterval(t *testing.T) {
	for d, ok := range map[time.Duration]bool{4 * time.Second: false, 5 * time.Second: true, 10 * time.Second: true, 5 * time.Minute: true, 6 * time.Minute: false} {
		if err := ValidateInterval(d); (err == nil) != ok {
			t.Errorf("ValidateInterval(%v) = %v, want ok=%v", d, err, ok)
		}
	}
}
