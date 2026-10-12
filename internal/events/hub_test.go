package events

import (
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	h := NewHub()
	id, ch := h.Subscribe()
	defer h.Unsubscribe(id)

	h.Publish("scan_started", map[string]any{"n": 1})
	ev := <-ch
	if ev.Type != "scan_started" || ev.ID != 1 {
		t.Errorf("event = %+v", ev)
	}
}

func TestSinceReplaysMissedEvents(t *testing.T) {
	h := NewHub()
	h.Publish("a", nil)
	h.Publish("b", nil)

	got := h.Since(1)
	if len(got) != 1 || got[0].Type != "b" {
		t.Errorf("Since(1) = %+v, want just b", got)
	}
	if all := h.Since(0); len(all) != 2 {
		t.Errorf("Since(0) = %d events, want 2", len(all))
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	h := NewHub()
	id, ch := h.Subscribe()
	h.Unsubscribe(id)
	if _, open := <-ch; open {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestRingBufferIsBounded(t *testing.T) {
	h := NewHub()
	for i := 0; i < ringSize+10; i++ {
		h.Publish("x", i)
	}
	if got := len(h.Since(0)); got != ringSize {
		t.Errorf("buffered events = %d, want %d", got, ringSize)
	}
}

func TestSlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	h := NewHub()
	id, _ := h.Subscribe() // never drained
	defer h.Unsubscribe(id)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			h.Publish("x", i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}
