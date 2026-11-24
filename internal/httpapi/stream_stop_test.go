package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/events"
)

// A clean stop used to take the whole ten-second budget and exit non-zero, because the server's own
// stop waits for handlers to return, does not cancel their contexts, and the SSE handler waits
// forever. Closing the hub is what ends them, and `main.go` registers it on the server; this is the
// same wiring, asserted.
func TestStopEndsAnOpenEventStream(t *testing.T) {
	srv, _, _, hub := newTestServer(t, Options{})
	ts := httptest.NewUnstartedServer(srv)
	// The wiring `main.go` uses, replicated here: the hub the handler subscribed to is closed when
	// the server is asked to stop.
	ts.Config.RegisterOnShutdown(hub.Close)
	ts.Start()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		done <- ts.Config.Shutdown(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stop returned %v, want a clean stop", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("stop never returned: the stream is still holding the server open")
	}
}

// The hub is what ends it, so the same thing holds at the hub's own level: closing disconnects
// every subscriber rather than leaving their channels open.
func TestClosingTheHubDisconnectsSubscribers(t *testing.T) {
	hub := events.NewHub()
	_, ch := hub.Subscribe()

	hub.Close()

	select {
	case _, open := <-ch:
		if open {
			t.Fatal("the channel is still open after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("the subscriber channel was never closed")
	}
	// Publishing after a close is not a panic and reaches nobody.
	hub.Publish("scan_complete", map[string]any{})
}
