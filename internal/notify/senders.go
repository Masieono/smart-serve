package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Delivery tuning from docs/NOTIFICATIONS.md: a 10 s timeout and two retries
// with a 5 s backoff on 5xx or network errors. The dispatcher's budget caps the
// worst case, so a dead endpoint cannot stall the next scheduled scan.
const (
	requestTimeout = 10 * time.Second
	sendAttempts   = 3
	maxResponse    = 4 << 10
)

// retryBackoff is a variable so tests can shorten it; the shipped value is the
// documented 5 s.
var retryBackoff = 5 * time.Second

// transientError marks a failure worth retrying: a network blip or a 5xx. A 4xx
// is a configuration problem, so retrying it would only waste the budget.
type transientError struct{ err error }

func (e transientError) Error() string { return e.err.Error() }
func (e transientError) Unwrap() error { return e.err }

func isTransient(err error) bool {
	var t transientError
	return errors.As(err, &t)
}

// newHTTPClient returns the client the senders share.
func newHTTPClient() *http.Client { return &http.Client{Timeout: requestTimeout} }

// doRequest sends a request built fresh for each attempt (so a retry always has
// a full body) and classifies the response.
func doRequest(ctx context.Context, client *http.Client, build func() (*http.Request, error)) error {
	return retry(ctx, sendAttempts, retryBackoff, func() error {
		req, err := build()
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return transientError{err}
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
		detail := strings.TrimSpace(string(body))
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return nil
		case resp.StatusCode >= 500:
			return transientError{fmt.Errorf("%s: %s", resp.Status, detail)}
		default:
			return fmt.Errorf("%s: %s", resp.Status, detail)
		}
	})
}

// retry runs fn up to `attempts` times, backing off between tries. Only transient
// failures are retried; a 4xx is a configuration problem, so repeating it would
// waste the budget.
func retry(ctx context.Context, attempts int, backoff time.Duration, fn func() error) error {
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if !isTransient(err) || attempt == attempts {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
	return err
}

// --- ntfy ---------------------------------------------------------------------

// ntfySender posts to an ntfy server (docs/NOTIFICATIONS.md).
type ntfySender struct{ client *http.Client }

// NewNtfy returns the ntfy channel sender.
func NewNtfy(client *http.Client) Sender {
	if client == nil {
		client = newHTTPClient()
	}
	return ntfySender{client: client}
}

func (ntfySender) Type() string { return "ntfy" }

func (s ntfySender) ValidateConfig(cfg map[string]any) error {
	if stringField(cfg, "topic") == "" {
		return errors.New("topic is required")
	}
	if server := stringField(cfg, "server"); server != "" {
		u, err := url.Parse(server)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("server must be an http(s) URL, got %q", server)
		}
	}
	return nil
}

func (s ntfySender) Send(ctx context.Context, cfg map[string]any, ev Event) error {
	if err := s.ValidateConfig(cfg); err != nil {
		return err
	}
	server := strings.TrimRight(stringField(cfg, "server"), "/")
	if server == "" {
		server = "https://ntfy.sh"
	}
	endpoint := server + "/" + url.PathEscape(stringField(cfg, "topic"))
	body := ev.Body()
	token := stringField(cfg, "token")

	return doRequest(ctx, s.client, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		// ntfy takes its metadata from headers, which cannot carry emoji, so the
		// title is reduced to ASCII here; the body keeps the full message.
		req.Header.Set("Title", ascii(ev.Title()))
		req.Header.Set("Priority", ntfyPriority(ev.Priority))
		req.Header.Set("Tags", strings.Join(ntfyTags(ev), ","))
		// One action, and only when the deployment has said where it is reachable (`FEATURES.md`
		// B10). `clear=true` dismisses the notification once it has been opened, which is the right
		// behaviour for "I have seen this": the alternative is a tray that accumulates alerts the
		// user has already dealt with.
		if link := ev.Link(); link != "" {
			label := "Open dashboard"
			if ev.Serial != "" {
				label = "Open drive"
			}
			req.Header.Set("Actions", fmt.Sprintf("view, %s, %s, clear=true", label, link))
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req, nil
	})
}

// ntfyPriority maps a Smart-Serve priority onto ntfy's 1–5 scale. The mapping is
// fixed in the MVP (docs/NOTIFICATIONS.md).
func ntfyPriority(p Priority) string {
	switch p {
	case PriorityHigh:
		return "5"
	case PriorityMedium:
		return "4"
	default:
		return "3"
	}
}

func ntfyTags(ev Event) []string {
	switch ev.Type {
	case EventHealthChange:
		if ev.NewHealth == "good" {
			return []string{"white_check_mark"}
		}
		return []string{"warning"}
	case EventTemperatureHigh:
		return []string{"thermometer"}
	case EventDeviceDisappeared, EventDeviceAppeared:
		return []string{"electric_plug"}
	case EventScanFailure:
		return []string{"rotating_light"}
	}
	return []string{"floppy_disk"}
}

// ascii strips anything outside printable ASCII, because HTTP header values
// cannot carry emoji or other non-Latin-1 characters.
func ascii(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// --- generic webhook ----------------------------------------------------------

// webhookSender posts the event as JSON to an arbitrary URL.
type webhookSender struct{ client *http.Client }

// NewWebhook returns the generic webhook channel sender.
func NewWebhook(client *http.Client) Sender {
	if client == nil {
		client = newHTTPClient()
	}
	return webhookSender{client: client}
}

func (webhookSender) Type() string { return "webhook" }

func (s webhookSender) ValidateConfig(cfg map[string]any) error {
	raw := stringField(cfg, "url")
	if raw == "" {
		return errors.New("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid url %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url must be http or https, got %q", u.Scheme)
	}
	if method := strings.ToUpper(stringField(cfg, "method")); method != "" &&
		method != http.MethodPost && method != http.MethodPut {
		return fmt.Errorf("method must be POST or PUT, got %q", method)
	}
	return nil
}

func (s webhookSender) Send(ctx context.Context, cfg map[string]any, ev Event) error {
	if err := s.ValidateConfig(cfg); err != nil {
		return err
	}
	raw := stringField(cfg, "url")
	method := strings.ToUpper(stringField(cfg, "method"))
	if method == "" {
		method = http.MethodPost
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	headers := stringMap(cfg["headers"])

	return doRequest(ctx, s.client, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, raw, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	})
}

// stringField reads a trimmed string from a decoded channel config.
func stringField(cfg map[string]any, key string) string {
	s, _ := cfg[key].(string)
	return strings.TrimSpace(s)
}

// intField reads an integer from a decoded channel config, accepting either a JSON
// number or a numeric string (the UI posts strings for text inputs).
func intField(cfg map[string]any, key string, def int) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// splitList reads a comma, semicolon or whitespace separated list, as used by the
// recipient fields.
func splitList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// timeFromMillis renders an event timestamp for a payload that wants RFC 3339,
// returning "" when there is no timestamp to report.
func timeFromMillis(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// stringMap reads a string→string map, as used by webhook headers.
func stringMap(v any) map[string]string {
	out := map[string]string{}
	switch m := v.(type) {
	case map[string]any:
		for k, val := range m {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
	case map[string]string:
		for k, val := range m {
			out[k] = val
		}
	}
	return out
}
