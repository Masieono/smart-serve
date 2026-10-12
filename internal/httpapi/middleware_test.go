package httpapi

import (
	"net/http"
	"testing"
)

func TestCSRFGuard(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	// A same-origin browser request is allowed (httptest sets Host: example.com).
	rec := do(t, srv, http.MethodPut, "/api/devices/S1/notes", `{"name":"x"}`,
		map[string]string{"Origin": "http://example.com"})
	if rec.Code != http.StatusOK {
		t.Errorf("same-origin status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// A cross-origin browser request is rejected.
	rec = do(t, srv, http.MethodPut, "/api/devices/S1/notes", `{"name":"x"}`,
		map[string]string{"Origin": "http://evil.example"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin status = %d, want 403", rec.Code)
	}

	// Cross-site fetch metadata is rejected even without an Origin header.
	rec = do(t, srv, http.MethodPost, "/api/scan", "",
		map[string]string{"Sec-Fetch-Site": "cross-site"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site status = %d, want 403", rec.Code)
	}

	// Non-browser clients send no Origin header, so curl and scripts keep working.
	rec = do(t, srv, http.MethodPut, "/api/devices/S1/notes", `{"name":"cli"}`, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("cli status = %d, want 200", rec.Code)
	}
}

func TestBasePath(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{BasePath: "/smartserve"})

	if rec := do(t, srv, http.MethodGet, "/smartserve/healthz", "", nil); rec.Code != http.StatusOK {
		t.Errorf("prefixed healthz status = %d, want 200", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/healthz", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unprefixed healthz status = %d, want 404", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/smartserve/api/devices", "", nil); rec.Code != http.StatusOK {
		t.Errorf("prefixed api status = %d, want 200", rec.Code)
	}
}
