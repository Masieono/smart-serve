package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// B10: the link back into the app.
//
// Empty rather than a guess when the deployment has not said where it is. A notification action
// pointing at `localhost` from a phone is worse than no action at all: it looks like a feature and
// fails like a bug, and the reader has no way to tell which it was.
func TestEventLinkIsEmptyWithoutAPublicURL(t *testing.T) {
	for _, ev := range []Event{
		{Type: EventHealthChange, Serial: "S1"},
		{Type: EventScanSuccess},
		{Type: EventGroupHeat, Group: "tank"},
	} {
		if got := ev.Link(); got != "" {
			t.Errorf("Link() = %q, want empty", got)
		}
	}
}

func TestEventLinkPointsAtTheDriveOrTheDashboard(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   Event
		want string
	}{
		{"drive", Event{Serial: "S1", baseURL: "https://nas.lan:8080"}, "https://nas.lan:8080/device/S1"},
		// A trailing slash must not produce a doubled one: this URL is what a phone opens.
		{"trailing slash", Event{Serial: "S1", baseURL: "https://nas.lan:8080/"}, "https://nas.lan:8080/device/S1"},
		{"dashboard", Event{baseURL: "https://nas.lan:8080"}, "https://nas.lan:8080/"},
		{"subpath", Event{Serial: "S1", baseURL: "https://nas.lan/smart-serve"}, "https://nas.lan/smart-serve/device/S1"},
	} {
		if got := tc.ev.Link(); got != tc.want {
			t.Errorf("%s: Link() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A serial with a slash or a space in it must not break the URL — USB bridges report serials with
// punctuation in them, and the drive page is addressed by serial.
func TestEventLinkEscapesTheSerial(t *testing.T) {
	ev := Event{Serial: "usb/0 space", baseURL: "https://nas.lan"}
	if got := ev.Link(); !strings.Contains(got, "usb%2F0%20space") {
		t.Errorf("Link() = %q, want the serial escaped", got)
	}
}

func TestNtfySenderAddsAnOpenActionWhenTheAppHasAURL(t *testing.T) {
	var gotActions string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotActions = r.Header.Get("Actions")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewNtfy(srv.Client())
	cfg := map[string]any{"server": srv.URL, "topic": "t"}

	ev := Event{Type: EventHealthChange, Priority: PriorityHigh, Serial: "S1", baseURL: "https://nas.lan"}
	if err := sender.Send(context.Background(), cfg, ev); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// `view` with `clear=true`: the notification is dismissed once it has been opened, because the
	// alternative is a tray that accumulates alerts the user has already dealt with.
	if !strings.Contains(gotActions, "view") ||
		!strings.Contains(gotActions, "https://nas.lan/device/S1") ||
		!strings.Contains(gotActions, "clear=true") {
		t.Errorf("Actions = %q, want a view action on the drive with clear", gotActions)
	}

	// And nothing at all when the deployment has not said where it is.
	gotActions = ""
	if err := sender.Send(context.Background(), cfg, Event{Type: EventScanSuccess}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotActions != "" {
		t.Errorf("Actions = %q, want none", gotActions)
	}
}
