// Package events is the in-process publish/subscribe hub that backs the SSE
// stream. It keeps a small ring buffer so a reconnecting browser can catch up
// using Last-Event-ID (docs/API.md, ADR-006).
package events

import "sync"

// ringSize bounds the replay buffer. Reconnecting clients older than this simply
// miss events; the UI then refreshes from the REST API.
const ringSize = 64

// Event is one server-sent event.
type Event struct {
	ID   int64
	Type string
	Data any
}

// Hub fans events out to subscribers.
type Hub struct {
	mu      sync.Mutex
	subs    map[int]chan Event
	nextSub int
	ring    []Event
	nextID  int64
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subs: map[int]chan Event{}}
}

// Subscribe registers a listener. Callers must Unsubscribe when done.
func (h *Hub) Subscribe() (int, <-chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	id := h.nextSub
	h.nextSub++
	ch := make(chan Event, 16)
	h.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a listener and closes its channel.
func (h *Hub) Unsubscribe(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ch, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(ch)
	}
}

// Publish delivers an event to every subscriber and records it for replay.
// Slow subscribers are skipped rather than blocking the publisher.
func (h *Hub) Publish(eventType string, data any) Event {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	ev := Event{ID: h.nextID, Type: eventType, Data: data}

	h.ring = append(h.ring, ev)
	if len(h.ring) > ringSize {
		h.ring = h.ring[len(h.ring)-ringSize:]
	}
	for _, ch := range h.subs {
		select {
		case ch <- ev:
		default: // subscriber is not keeping up; drop for that subscriber
		}
	}
	return ev
}

// Since returns buffered events with an id greater than lastID, for Last-Event-ID
// replay.
func (h *Hub) Since(lastID int64) []Event {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]Event, 0, len(h.ring))
	for _, ev := range h.ring {
		if ev.ID > lastID {
			out = append(out, ev)
		}
	}
	return out
}
