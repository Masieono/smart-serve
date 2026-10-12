package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScanReturnsAccepted(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodPost, "/api/scan", "", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
	}
	decodeBody(t, rec, &resp)
	if resp.Status != "started" && resp.Status != "joined" {
		t.Errorf("status = %q", resp.Status)
	}
}

func TestScanHistory(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodGet, "/api/scans", "", nil)
	var resp struct {
		Scans []struct {
			ID           int64  `json:"id"`
			Trigger      string `json:"trigger"`
			OK           bool   `json:"ok"`
			DevicesTotal int    `json:"devices_total"`
			DevicesOK    int    `json:"devices_ok"`
		} `json:"scans"`
	}
	decodeBody(t, rec, &resp)
	if len(resp.Scans) != 1 {
		t.Fatalf("scans = %+v", resp.Scans)
	}
	s := resp.Scans[0]
	if !s.OK || s.DevicesTotal != 1 || s.DevicesOK != 1 || s.Trigger != "manual" {
		t.Errorf("scan = %+v", s)
	}
}

func TestEventsStream(t *testing.T) {
	srv, _, _, hub := newTestServer(t, Options{})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q", ct)
	}
	if ab := resp.Header.Get("X-Accel-Buffering"); ab != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", ab)
	}

	events := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				events <- strings.TrimSpace(name)
				return
			}
		}
	}()

	// Publish until the (asynchronously subscribing) client observes one.
	pubDone := make(chan struct{})
	defer close(pubDone)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-pubDone:
				return
			case <-ticker.C:
				hub.Publish("scan_started", map[string]any{"n": 1})
			}
		}
	}()

	select {
	case got := <-events:
		if got != "scan_started" {
			t.Errorf("event = %q, want scan_started", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an SSE event")
	}
}
