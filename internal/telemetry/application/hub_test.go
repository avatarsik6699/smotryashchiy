package application

import (
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
)

func metricMsg(host string) Message {
	return Message{Type: TypeMetric, HostID: host, Metric: &domain.Metric{Name: "a.b"}}
}

func TestHubDeliversOnlyMatchingMessages(t *testing.T) {
	h := NewHub()
	all, hostA, checks := h.Subscribe("", ""), h.Subscribe("A", ""), h.Subscribe("", TypeCheck)
	h.Publish([]Message{metricMsg("A"), metricMsg("B")})
	if len(all.C) != 2 || len(hostA.C) != 1 || len(checks.C) != 0 {
		t.Fatalf("all=%d hostA=%d checks=%d, want 2/1/0", len(all.C), len(hostA.C), len(checks.C))
	}
}

func TestHubDropsSlowSubscriberWithoutBlocking(t *testing.T) {
	h := NewHub()
	slow, fast := h.Subscribe("", ""), h.Subscribe("", "")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < SubscriberBuffer+10; i++ {
			h.Publish([]Message{metricMsg("A")})
			for len(fast.C) > 0 { // the fast client keeps up
				<-fast.C
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
	n := 0
	for range slow.C { // drains buffered messages, then observes the close
		n++
	}
	if n != SubscriberBuffer || !slow.Dropped() {
		t.Fatalf("slow got %d messages, dropped = %v; want %d and dropped", n, slow.Dropped(), SubscriberBuffer)
	}
	h.Publish([]Message{metricMsg("A")})
	if len(fast.C) != 1 {
		t.Fatal("fast subscriber should survive the slow one being dropped")
	}
}

func TestHubCloseAndUnsubscribeAreSafe(t *testing.T) {
	h := NewHub()
	sub := h.Subscribe("", "")
	h.Unsubscribe(sub)
	h.Unsubscribe(sub) // second call is a no-op
	other := h.Subscribe("", "")
	h.Close()
	if _, ok := <-other.C; ok || other.Dropped() {
		t.Fatal("Close must close subscribers without marking them dropped")
	}
	if _, ok := <-h.Subscribe("", "").C; ok {
		t.Fatal("subscribing after Close must yield a closed subscription")
	}
	h.Publish([]Message{metricMsg("A")}) // no panic
}
