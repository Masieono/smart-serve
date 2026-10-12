package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// shortBackoff keeps retry tests fast without changing the shipped value.
func shortBackoff(t *testing.T) {
	t.Helper()
	original := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = original })
}

func TestNtfySenderPostsToTheTopic(t *testing.T) {
	var (
		mu                   sync.Mutex
		gotPath, gotTitle    string
		gotPriority, gotAuth string
		gotBody              string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		gotPath = r.URL.Path
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		gotAuth = r.Header.Get("Authorization")
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewNtfy(srv.Client())
	cfg := map[string]any{"server": srv.URL, "topic": "my-alerts", "token": "tok"}
	ev := Event{
		Type: EventHealthChange, Priority: PriorityHigh,
		DeviceName: "boot", OldHealth: "good", NewHealth: "bad", Serial: "S1",
	}
	if err := sender.Send(context.Background(), cfg, ev); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/my-alerts" {
		t.Errorf("path = %q, want /my-alerts", gotPath)
	}
	if gotTitle != "Drive health changed: boot" {
		t.Errorf("Title header = %q", gotTitle)
	}
	if gotPriority != "5" {
		t.Errorf("Priority header = %q, want 5 for a high-priority event", gotPriority)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want the bearer token", gotAuth)
	}
	if !strings.Contains(gotBody, "good → bad") {
		t.Errorf("body = %q, want the transition", gotBody)
	}
}

// The ntfy Title header cannot carry emoji, so the sender must strip them rather
// than let the request fail.
func TestNtfySenderKeepsTheTitleHeaderASCII(t *testing.T) {
	var gotTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewNtfy(srv.Client())
	cfg := map[string]any{"server": srv.URL, "topic": "t"}
	ev := Event{Type: EventDeviceAppeared, DeviceName: "bööt-drive"}
	if err := sender.Send(context.Background(), cfg, ev); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if strings.ContainsRune(gotTitle, 'ö') {
		t.Errorf("Title = %q, want non-ASCII stripped", gotTitle)
	}
}

func TestNtfyPriorityMapping(t *testing.T) {
	cases := map[Priority]string{PriorityLow: "3", PriorityMedium: "4", PriorityHigh: "5"}
	for priority, want := range cases {
		if got := ntfyPriority(priority); got != want {
			t.Errorf("ntfyPriority(%q) = %q, want %q", priority, got, want)
		}
	}
}

func TestWebhookSenderPostsJSON(t *testing.T) {
	var (
		mu                        sync.Mutex
		gotMethod, gotCT, gotAuth string
		payload                   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		mu.Lock()
		defer mu.Unlock()
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		payload = decoded
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sender := NewWebhook(srv.Client())
	cfg := map[string]any{
		"url":     srv.URL,
		"method":  "put",
		"headers": map[string]any{"Authorization": "Bearer xyz"},
	}
	ev := Event{Type: EventScanFailure, Priority: PriorityMedium, ScheduleID: 3, Error: "boom"}
	if err := sender.Send(context.Background(), cfg, ev); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("content type = %q", gotCT)
	}
	if gotAuth != "Bearer xyz" {
		t.Errorf("Authorization = %q, want the configured header", gotAuth)
	}
	if payload["event"] != EventScanFailure || payload["priority"] != string(PriorityMedium) {
		t.Errorf("payload = %v", payload)
	}
	// The squelch key is internal bookkeeping and must not leak onto the wire.
	if _, leaked := payload["key"]; leaked {
		t.Errorf("payload leaked the squelch key: %v", payload)
	}
}

func TestValidateConfig(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewNtfy(nil))
	reg.Register(NewWebhook(nil))

	bad := []struct {
		typ string
		cfg map[string]any
	}{
		{"ntfy", map[string]any{}},                                    // no topic
		{"ntfy", map[string]any{"topic": "t", "server": "not a url"}}, // unparseable
		{"ntfy", map[string]any{"topic": "t", "server": "ftp://x/y"}}, // wrong scheme
		{"webhook", map[string]any{}},                                 // no url
		{"webhook", map[string]any{"url": "ftp://example.com"}},       // wrong scheme
		{"webhook", map[string]any{"url": "https://e.com", "method": "DELETE"}},
		{"carrier-pigeon", map[string]any{}}, // unregistered type
	}
	for _, c := range bad {
		if err := reg.ValidateConfig(c.typ, c.cfg); err == nil {
			t.Errorf("ValidateConfig(%s, %v) should have failed", c.typ, c.cfg)
		}
	}

	good := []struct {
		typ string
		cfg map[string]any
	}{
		{"ntfy", map[string]any{"topic": "alerts"}}, // server defaults to ntfy.sh
		{"ntfy", map[string]any{"topic": "alerts", "server": "https://n.example.com"}},
		{"webhook", map[string]any{"url": "https://example.com/hook"}}, // method defaults to POST
		{"webhook", map[string]any{"url": "http://example.com/hook", "method": "put"}},
	}
	for _, c := range good {
		if err := reg.ValidateConfig(c.typ, c.cfg); err != nil {
			t.Errorf("ValidateConfig(%s, %v) = %v, want nil", c.typ, c.cfg, err)
		}
	}
}

func TestRetryOnlyRepeatsTransientFailures(t *testing.T) {
	ctx := context.Background()

	// A transient failure is retried, and a later success ends the loop.
	attempts := 0
	err := retry(ctx, 3, time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return transientError{errors.New("connection reset")}
		}
		return nil
	})
	if err != nil {
		t.Errorf("err = %v, want nil after a retry succeeds", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}

	// A permanent failure stops immediately: repeating it would only burn budget.
	attempts = 0
	err = retry(ctx, 3, time.Millisecond, func() error {
		attempts++
		return errors.New("400 Bad Request")
	})
	if err == nil {
		t.Error("want an error")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 for a permanent failure", attempts)
	}

	// A transient failure that never clears is retried exactly `attempts` times.
	attempts = 0
	if err := retry(ctx, 3, time.Millisecond, func() error {
		attempts++
		return transientError{errors.New("still down")}
	}); err == nil {
		t.Error("want an error after exhausting the attempts")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetryStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	attempts := 0
	err := retry(ctx, 3, time.Hour, func() error {
		attempts++
		return transientError{errors.New("down")}
	})
	if err == nil {
		t.Error("want an error")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 — the backoff must not outlive the context", attempts)
	}
}

// doRequest is where HTTP status codes become retry decisions, so it is worth
// pinning down: 2xx is success, 4xx is permanent, 5xx is worth repeating.
func TestDoRequestClassifiesResponses(t *testing.T) {
	shortBackoff(t)

	var mu sync.Mutex
	counts := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/bad":
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	build := func(path string) func() (*http.Request, error) {
		return func() (*http.Request, error) {
			return http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader("x"))
		}
	}

	if err := doRequest(context.Background(), client, build("/ok")); err != nil {
		t.Errorf("2xx: err = %v, want nil", err)
	}
	if err := doRequest(context.Background(), client, build("/bad")); err == nil {
		t.Error("4xx: want an error")
	} else if isTransient(err) {
		t.Errorf("4xx: err = %v, want it classified as permanent", err)
	}
	if err := doRequest(context.Background(), client, build("/boom")); err == nil {
		t.Error("5xx: want an error")
	} else if !isTransient(err) {
		t.Errorf("5xx: err = %v, want it classified as transient", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if counts["/ok"] != 1 {
		t.Errorf("successful requests = %d, want 1", counts["/ok"])
	}
	if counts["/bad"] != 1 {
		t.Errorf("4xx requests = %d, want 1 (no retry)", counts["/bad"])
	}
	if counts["/boom"] != sendAttempts {
		t.Errorf("5xx requests = %d, want %d (retried)", counts["/boom"], sendAttempts)
	}
}

func TestAsciiStripsNonPrintableCharacters(t *testing.T) {
	if got := ascii("  🌡️ hot drive  "); got != "hot drive" {
		t.Errorf("ascii = %q, want %q", got, "hot drive")
	}
	if got := ascii("plain"); got != "plain" {
		t.Errorf("ascii = %q, want %q", got, "plain")
	}
}

func TestStringMapReadsHeaderMaps(t *testing.T) {
	if got := stringMap(map[string]any{"A": "1", "B": 2}); len(got) != 1 || got["A"] != "1" {
		t.Errorf("stringMap = %v, want only the string entry", got)
	}
	if got := stringMap("nonsense"); len(got) != 0 {
		t.Errorf("stringMap = %v, want empty", got)
	}
}
