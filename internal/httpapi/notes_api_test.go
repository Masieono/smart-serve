package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/store"
)

// seedDevice puts one drive in the store with a name, so a test can prove the name survived.
func seedDevice(t *testing.T, st *store.Store, serial string) {
	t.Helper()
	now := time.Now()
	if _, err := st.SaveScan(context.Background(), "manual", now, now,
		[]store.ScannedDevice{{Serial: serial, Health: "good", Score: 100}}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
	if err := st.SetNotes(context.Background(), store.Notes{Serial: serial, Name: "kept"}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
}

// A body of `null` decodes into a struct without error and leaves every field at zero, which for a
// full-replace endpoint means "clear everything". A drive's notes, name, tags and bay were erased by
// `PUT .../notes` with that body.
func TestNullBodyIsRefused(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	seedDevice(t, st, "N1")

	rec := do(t, srv, http.MethodPut, "/api/devices/N1/notes", `null`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	dev, err := st.GetDevice(context.Background(), "N1")
	if err != nil || dev == nil {
		t.Fatalf("device: %v", err)
	}
	if dev.Name != "kept" {
		t.Errorf("name = %q, the body was applied anyway", dev.Name)
	}
}

// Trailing data after the first JSON value used to be ignored, so `{...}garbage` was accepted.
func TestTrailingGarbageIsRefused(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	seedDevice(t, st, "N2")

	rec := do(t, srv, http.MethodPut, "/api/devices/N2/notes", `{"name":"x"}junk`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

// An unknown serial used to answer 200 and create a notes row attached to nothing, so a drive
// forgotten in another tab came back hidden, with its old name and its old -d override.
func TestWritingForAnUnknownDeviceIs404(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	for _, c := range []struct {
		method, path, body string
	}{
		{http.MethodPut, "/api/devices/GONE/notes", `{"name":"x"}`},
		{http.MethodPut, "/api/devices/GONE/transport", `{"smart_d":"sat"}`},
		{http.MethodDelete, "/api/devices/GONE/ack", ""},
	} {
		rec := do(t, srv, c.method, c.path, c.body, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", c.method, c.path, rec.Code)
		}
	}
}
