package httpapi

import (
	"net/http"
	"testing"
)

// TestHistory returns the stored series, and honours the window it is given.
//
// These two endpoints were the last at zero coverage (`CHANGELOG.md`, known limitations). They are
// small, which is exactly why they were skipped and exactly why they are worth pinning: a history
// endpoint that silently returns the wrong window looks like a drive with no history.
func TestHistory(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodGet, "/api/devices/S1/history", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Serial string `json:"serial"`
		Series []struct {
			TS           string `json:"ts"`
			Score        *int   `json:"score"`
			Health       string `json:"health"`
			TemperatureC *int   `json:"temperature_c"`
			UsedPct      *int   `json:"used_pct"`
		} `json:"series"`
	}
	decodeBody(t, rec, &body)

	if body.Serial != "S1" {
		t.Errorf("serial = %q, want S1", body.Serial)
	}
	if len(body.Series) != 1 {
		t.Fatalf("series has %d points, want 1 after one scan", len(body.Series))
	}
	point := body.Series[0]
	if point.TS == "" || point.Health == "" || point.Score == nil {
		t.Errorf("point is missing its figures: %+v", point)
	}

	// A window that has not started yet has no readings in it. This is the query parameter the
	// endpoint exists for, so an unparsed or ignored `from` would be invisible without it.
	rec = do(t, srv, http.MethodGet, "/api/devices/S1/history?from=2099-01-01T00:00:00Z", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("future window status = %d, want 200", rec.Code)
	}
	decodeBody(t, rec, &body)
	if len(body.Series) != 0 {
		t.Errorf("future window returned %d points, want 0", len(body.Series))
	}

	// An unparseable timestamp falls back to the default window rather than erroring: the UI sends
	// what it has, and a stray value should not blank the page.
	rec = do(t, srv, http.MethodGet, "/api/devices/S1/history?from=yesterday", "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("unparseable window status = %d, want 200", rec.Code)
	}
}

// TestSetTransport covers the endpoint that stores the `-d` device type, including the validation
// that keeps it a device type rather than arbitrary text on a command line.
func TestSetTransport(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"a plain type", `{"smart_d":"sat"}`, http.StatusOK},
		{"a type with a sub-argument", `{"smart_d":"sat,12"}`, http.StatusOK},
		{"a vendor bridge", `{"smart_d":"usbjmicron,0"}`, http.StatusOK},
		{"clearing the override", `{"smart_d":""}`, http.StatusOK},
		{"a flag", `{"smart_d":"--save-smart"}`, http.StatusBadRequest},
		{"whitespace", `{"smart_d":"sat 12"}`, http.StatusBadRequest},
		{"a shell metacharacter", `{"smart_d":"sat;reboot"}`, http.StatusBadRequest},
		{"too long", `{"smart_d":"sat,aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, srv, http.MethodPut, "/api/devices/S1/transport", tc.body, nil)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
