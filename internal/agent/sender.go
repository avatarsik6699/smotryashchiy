package agent

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/agent/spool"
)

// Retry policy (docs/SPEC.md §4c).
const (
	backoffMin        = time.Second
	backoffMax        = time.Minute
	rebuildAfterFails = 3
)

// Uplink is what the sender needs from a tunnel session; *Session implements it.
type Uplink interface {
	Send(ctx context.Context, key string, batch []byte) (Result, error)
	Close()
}

// Dialer opens a new Uplink.
type Dialer func(ctx context.Context) (Uplink, error)

// Sender drains the spool oldest-first over one persistent tunnel session.
type Sender struct {
	spool *spool.Spool
	dial  Dialer
	log   *slog.Logger

	up    Uplink
	fails int // consecutive failures
}

// NewSender returns a Sender that dials lazily.
func NewSender(sp *spool.Spool, dial Dialer, log *slog.Logger) *Sender {
	return &Sender{spool: sp, dial: dial, log: log}
}

// Failures is the number of consecutive failed attempts (0 after any success).
func (s *Sender) Failures() int { return s.fails }

// Close releases the tunnel session, if any.
func (s *Sender) Close() {
	if s.up != nil {
		s.up.Close()
		s.up = nil
	}
}

// Drain sends queued batches until the spool is empty. It returns nil when empty and an error when
// the caller should back off. A batch leaves the spool only after the server answered 200, or
// answered that it can never accept it (400/413, dropped and logged so it cannot block the queue).
func (s *Sender) Drain(ctx context.Context) error {
	for {
		entry, ok, err := s.spool.Peek()
		if err != nil {
			return s.fail(err)
		}
		if !ok {
			s.fails = 0
			return nil
		}
		if s.up == nil {
			up, err := s.dial(ctx)
			if err != nil {
				return s.fail(err)
			}
			s.up = up
		}
		res, err := s.up.Send(ctx, entry.Key, entry.Batch)
		if err != nil {
			var status *StatusError
			if errors.As(err, &status) && (status.Code == http.StatusBadRequest || status.Code == http.StatusRequestEntityTooLarge) {
				hint := ""
				if status.Code == http.StatusBadRequest {
					hint = " (if this repeats, check that the host clock is not more than 5 minutes ahead)"
				}
				s.log.Error("server permanently rejected a batch; dropping it"+hint, "status", status.Code, "error", status.Body, "key", entry.Key)
				if err := s.spool.Remove(entry); err != nil {
					return s.fail(err)
				}
				continue
			}
			return s.fail(err)
		}
		if err := s.spool.Remove(entry); err != nil {
			return s.fail(err)
		}
		s.fails = 0
		s.log.Debug("batch delivered", "key", entry.Key, "accepted_metrics", res.Accepted.Metrics, "replayed", res.Replayed, "queued", s.spool.Len())
	}
}

// fail records a failure and rebuilds the tunnel after rebuildAfterFails in a row.
func (s *Sender) fail(err error) error {
	s.fails++
	if s.fails >= rebuildAfterFails {
		s.Close()
	}
	return err
}

// Backoff is the wait before retry number n (1-based): 1 s doubling up to 60 s, spread by ±20 %
// using jitter in [0,1).
func Backoff(n int, jitter float64) time.Duration {
	if n < 1 {
		n = 1
	}
	d := backoffMin
	for i := 1; i < n && d < backoffMax; i++ {
		d *= 2
	}
	if d > backoffMax {
		d = backoffMax
	}
	return time.Duration(float64(d) * (0.8 + 0.4*jitter))
}

// Run drains whenever kicked and retries with backoff after failures, until ctx is done.
func (s *Sender) Run(ctx context.Context, kick <-chan struct{}) {
	defer s.Close()
	for {
		if err := s.Drain(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			wait := Backoff(s.fails, rand.Float64())
			s.log.Warn("could not deliver queued batches; will retry", "error", err, "consecutive_failures", s.fails, "queued", s.spool.Len(), "retry_in", wait.Round(time.Millisecond))
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-kick:
		}
	}
}
