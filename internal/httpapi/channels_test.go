package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/notify"
	"github.com/masieono/smart-serve/internal/store"
)

// fakeNotifier stands in for the real dispatcher so API tests never reach the
// network.
type fakeNotifier struct {
	sent    []store.Channel
	outcome notify.Outcome
	err     error
	types   []string
}

func (f *fakeNotifier) SendTest(_ context.Context, ch store.Channel, _ notify.Event) (notify.Outcome, error) {
	f.sent = append(f.sent, ch)
	return f.outcome, f.err
}

func (f *fakeNotifier) ValidateConfig(typ string, cfg map[string]any) error {
	if typ == "ntfy" && cfg["topic"] == nil {
		return errors.New("topic is required")
	}
	return nil
}

func (f *fakeNotifier) Types() []string { return f.types }

// fakeScheduler records the API's nudges instead of running scans.
type fakeScheduler struct {
	ran   []int64
	wakes int
}

func (f *fakeScheduler) Wake() { f.wakes++ }

func (f *fakeScheduler) RunNow(_ context.Context, scheduleID int64) error {
	f.ran = append(f.ran, scheduleID)
	return nil
}

func createChannel(t *testing.T, srv *Server, body string) int64 {
	t.Helper()
	rec := do(t, srv, http.MethodPost, "/api/channels", body, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create channel status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Channel struct {
			ID int64 `json:"id"`
		} `json:"channel"`
	}
	decodeBody(t, rec, &created)
	return created.Channel.ID
}

func TestTestChannelDelivers(t *testing.T) {
	notifier := &fakeNotifier{outcome: notify.OutcomeSent, types: []string{"ntfy"}}
	srv, _, _, _ := newTestServer(t, Options{Notifier: notifier})
	id := createChannel(t, srv, `{"type":"ntfy","label":"alerts","config":{"topic":"t"}}`)

	rec := do(t, srv, http.MethodPost, fmt.Sprintf("/api/channels/%d/test", id), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
	}
	decodeBody(t, rec, &resp)
	if resp.Status != string(notify.OutcomeSent) {
		t.Errorf("status = %q, want %q", resp.Status, notify.OutcomeSent)
	}
	if len(notifier.sent) != 1 || notifier.sent[0].ID != id {
		t.Errorf("deliveries = %+v, want one for channel %d", notifier.sent, id)
	}
}

// A delivery failure must be reported with its reason, not swallowed: finding out
// *why* a channel is broken is the entire point of "send test".
func TestTestChannelReportsADeliveryFailure(t *testing.T) {
	notifier := &fakeNotifier{
		outcome: notify.OutcomeFailed,
		err:     errors.New("502 Bad Gateway"),
		types:   []string{"ntfy"},
	}
	srv, _, _, _ := newTestServer(t, Options{Notifier: notifier})
	id := createChannel(t, srv, `{"type":"ntfy","label":"alerts","config":{"topic":"t"}}`)

	rec := do(t, srv, http.MethodPost, fmt.Sprintf("/api/channels/%d/test", id), "", nil)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "502 Bad Gateway") {
		t.Errorf("body = %s, want the delivery error", body)
	}
}

// Without a notifier wired in there is nothing to test with, and saying so beats
// pretending the send worked.
func TestTestChannelWithoutANotifierIsNotImplemented(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	id := createChannel(t, srv, `{"type":"ntfy","label":"alerts","config":{"topic":"t"}}`)

	rec := do(t, srv, http.MethodPost, fmt.Sprintf("/api/channels/%d/test", id), "", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", rec.Code)
	}
}

func TestTestChannelUnknownID(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{Notifier: &fakeNotifier{types: []string{"ntfy"}}})

	rec := do(t, srv, http.MethodPost, "/api/channels/999/test", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// A bad config is rejected at the point of editing, not discovered at 3 a.m.
func TestChannelConfigIsValidatedOnSave(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{Notifier: &fakeNotifier{types: []string{"ntfy"}}})

	rec := do(t, srv, http.MethodPost, "/api/channels", `{"type":"ntfy","label":"bad","config":{}}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "topic") {
		t.Errorf("body = %s, want the validation reason", body)
	}

	id := createChannel(t, srv, `{"type":"ntfy","label":"ok","config":{"topic":"t"}}`)
	rec = do(t, srv, http.MethodPut, fmt.Sprintf("/api/channels/%d", id), `{"config":{}}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("update status = %d, want 400 for an invalid config", rec.Code)
	}
}

func TestNotificationsEndpointListsTheLogAndTypes(t *testing.T) {
	notifier := &fakeNotifier{types: []string{"ntfy", "webhook"}}
	srv, st, _, _ := newTestServer(t, Options{Notifier: notifier})
	id := createChannel(t, srv, `{"type":"ntfy","label":"alerts","config":{"topic":"t"}}`)

	if err := st.LogNotification(context.Background(), id, notify.EventHealthChange,
		"health_change:S1:caution", time.Now(), string(notify.OutcomeSent), ""); err != nil {
		t.Fatalf("LogNotification: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/notifications", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Notifications []struct {
			ChannelID int64  `json:"channel_id"`
			EventType string `json:"event_type"`
			Status    string `json:"status"`
		} `json:"notifications"`
		EventTypes   []string `json:"event_types"`
		ChannelTypes []string `json:"channel_types"`
	}
	decodeBody(t, rec, &resp)

	if len(resp.Notifications) != 1 || resp.Notifications[0].EventType != notify.EventHealthChange {
		t.Errorf("notifications = %+v", resp.Notifications)
	}
	if resp.Notifications[0].ChannelID != id || resp.Notifications[0].Status != string(notify.OutcomeSent) {
		t.Errorf("notification = %+v", resp.Notifications[0])
	}
	if len(resp.EventTypes) != len(notify.EventTypes) {
		t.Errorf("event types = %v, want the full catalog %v", resp.EventTypes, notify.EventTypes)
	}
	if len(resp.ChannelTypes) != 2 {
		t.Errorf("channel types = %v, want [ntfy webhook]", resp.ChannelTypes)
	}
}

// Run now must not fire a scan when a scheduler owns the timings — it hands the
// work over, so last-run and next-run are recorded by the same path as a timed
// fire.
func TestRunScheduleDelegatesToTheScheduler(t *testing.T) {
	sched := &fakeScheduler{}
	srv, _, runner, _ := newTestServer(t, Options{Scheduler: sched})

	rec := do(t, srv, http.MethodPost, "/api/schedules", `{"enabled":true,"kind":"interval","every_seconds":3600,"label":"hourly"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Schedule struct {
			ID int64 `json:"id"`
		} `json:"schedule"`
	}
	decodeBody(t, rec, &created)

	rec = do(t, srv, http.MethodPost, fmt.Sprintf("/api/schedules/%d/run", created.Schedule.ID), "", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(sched.ran) != 1 || sched.ran[0] != created.Schedule.ID {
		t.Errorf("scheduler runs = %v, want [%d]", sched.ran, created.Schedule.ID)
	}
	if runner.calls != 0 {
		t.Errorf("runner calls = %d, want 0 — the scheduler owns the scan", runner.calls)
	}
}

// A schedule change must nudge the scheduler so it takes effect immediately.
func TestScheduleChangesWakeTheScheduler(t *testing.T) {
	sched := &fakeScheduler{}
	srv, _, _, _ := newTestServer(t, Options{Scheduler: sched})

	createChannel(t, srv, `{"type":"ntfy","label":"x","config":{"topic":"t"}}`) // unrelated, must not wake
	if sched.wakes != 0 {
		t.Fatalf("wakes = %d, want 0 before any schedule change", sched.wakes)
	}

	rec := do(t, srv, http.MethodPost, "/api/schedules", `{"enabled":true,"kind":"interval","every_seconds":60,"label":"x"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}
	if sched.wakes != 1 {
		t.Errorf("wakes = %d, want 1 after creating a schedule", sched.wakes)
	}

	var created struct {
		Schedule struct {
			ID int64 `json:"id"`
		} `json:"schedule"`
	}
	decodeBody(t, rec, &created)

	do(t, srv, http.MethodPut, fmt.Sprintf("/api/schedules/%d", created.Schedule.ID), `{"enabled":false}`, nil)
	do(t, srv, http.MethodDelete, fmt.Sprintf("/api/schedules/%d", created.Schedule.ID), "", nil)
	if sched.wakes != 3 {
		t.Errorf("wakes = %d, want 3 after create, update and delete", sched.wakes)
	}
}
