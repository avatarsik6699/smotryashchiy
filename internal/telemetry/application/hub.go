package application

import (
	"sync"

	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

// Stream message types (docs/SPEC.md §4.6).
const (
	TypeMetric = "metric"
	TypeCheck  = "check"
	TypeEvent  = "event"
	// TypeUptime carries an uptime probe result (docs/SPEC.md §4e); it belongs to a target, not a host.
	TypeUptime = "uptime"
)

// SubscriberBuffer is the per-client queue length; a client that lets it fill is disconnected.
const SubscriberBuffer = 64

// Message is one newly accepted record. Exactly the field matching Type is set.
type Message struct {
	Type   string
	HostID string
	Metric *domain.Metric
	Check  *domain.Check
	Event  *domain.Event
	// TargetID and Payload are set for TypeUptime; Payload is already JSON-ready.
	TargetID string
	Payload  any
}

// Publisher receives the records an ingest call accepted.
type Publisher interface{ Publish(msgs []Message) }

// Hub fans messages out to live subscribers. Publishing never blocks: a subscriber whose buffer is
// full is dropped, so a slow client cannot stall ingest.
type Hub struct {
	mu     sync.Mutex
	subs   map[*Subscription]struct{}
	closed bool
}

// NewHub returns an empty Hub.
func NewHub() *Hub { return &Hub{subs: map[*Subscription]struct{}{}} }

// Subscription is one live consumer. Read C until it is closed; Dropped then tells whether the
// hub disconnected it for being too slow (as opposed to Unsubscribe or hub shutdown).
type Subscription struct {
	C       chan Message
	host    string
	typ     string
	dropped bool
}

// Dropped reports whether the subscription was closed because its buffer overflowed. Only valid
// after C is closed.
func (s *Subscription) Dropped() bool { return s.dropped }

// Subscribe registers a consumer, optionally filtered by host ID and message type ("" = all).
// After Close the returned subscription is already closed.
func (h *Hub) Subscribe(host, typ string) *Subscription {
	sub := &Subscription{C: make(chan Message, SubscriberBuffer), host: host, typ: typ}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(sub.C)
		return sub
	}
	h.subs[sub] = struct{}{}
	return sub
}

// Unsubscribe removes sub and closes its channel; it is safe to call after a drop.
func (h *Hub) Unsubscribe(sub *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[sub]; ok {
		delete(h.subs, sub)
		close(sub.C)
	}
}

// Publish delivers msgs to every matching subscriber without blocking.
func (h *Hub) Publish(msgs []Message) {
	if len(msgs) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		for _, m := range msgs {
			if (sub.host != "" && sub.host != m.HostID) || (sub.typ != "" && sub.typ != m.Type) {
				continue
			}
			select {
			case sub.C <- m:
			default:
				sub.dropped = true
				delete(h.subs, sub)
				close(sub.C)
			}
			if sub.dropped {
				break
			}
		}
	}
}

// Close disconnects every subscriber and refuses new ones (server shutdown).
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for sub := range h.subs {
		delete(h.subs, sub)
		close(sub.C)
	}
}

// messagesFor converts an accepted batch into stream messages.
func messagesFor(hostID string, b domain.Batch) []Message {
	msgs := make([]Message, 0, b.Len())
	for i := range b.Metrics {
		msgs = append(msgs, Message{Type: TypeMetric, HostID: hostID, Metric: &b.Metrics[i]})
	}
	for i := range b.Checks {
		msgs = append(msgs, Message{Type: TypeCheck, HostID: hostID, Check: &b.Checks[i]})
	}
	for i := range b.Events {
		msgs = append(msgs, Message{Type: TypeEvent, HostID: hostID, Event: &b.Events[i]})
	}
	return msgs
}

// SubscriberCount returns the number of live subscribers (for tests and diagnostics).
func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
