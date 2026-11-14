package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestSecurityHeaders covers the headers that constrain a response rather than describe
// it. They are asserted on an allowed request, on one the CSRF guard rejects, and on a
// 404 — a rejected request is still a document a browser renders, so it gets the same
// treatment as a successful one.
func TestSecurityHeaders(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	for _, tc := range []struct {
		name   string
		method string
		path   string
		header map[string]string
		want   int
	}{
		{name: "allowed", method: http.MethodGet, path: "/api/devices", want: http.StatusOK},
		{name: "rejected", method: http.MethodPost, path: "/api/scan", want: http.StatusForbidden,
			header: map[string]string{"Origin": "http://evil.example"}},
		{name: "not found", method: http.MethodGet, path: "/api/nope", want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, srv, tc.method, tc.path, "", tc.header)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			for header, want := range map[string]string{
				"X-Content-Type-Options": "nosniff",
				"Referrer-Policy":        "no-referrer",
				"X-Frame-Options":        "DENY",
			} {
				if got := rec.Header().Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
			csp := rec.Header().Get("Content-Security-Policy")
			for _, directive := range []string{"default-src 'self'", "script-src 'self'", "base-uri 'none'"} {
				if !strings.Contains(csp, directive) {
					t.Errorf("CSP %q is missing %q", csp, directive)
				}
			}
			// The one directive that would let injected markup run must stay strict.
			if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") ||
				strings.Contains(csp, "script-src 'unsafe-inline'") {
				t.Errorf("CSP allows inline script: %q", csp)
			}
		})
	}
}

// panicServer is the smallest thing that can panic behind the middleware.
func panicServer(logw *bytes.Buffer, handler http.HandlerFunc) http.Handler {
	logger := slog.New(slog.NewTextHandler(logw, nil))
	return recoverMiddleware(logger, handler)
}

func TestRecoverMiddlewareReturnsJSON(t *testing.T) {
	var logw bytes.Buffer
	h := panicServer(&logw, func(http.ResponseWriter, *http.Request) {
		panic("map is nil in the worst possible place")
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/devices/S1", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type = %q, want JSON", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error"`) {
		t.Errorf("body = %q, want a JSON error object", body)
	}
	// The panic value is an internal detail, and the client already knows it failed.
	if body := rec.Body.String(); strings.Contains(body, "map is nil") {
		t.Errorf("body leaks the panic value: %q", body)
	}

	// ...but the log has to carry it, with the request attached, or the trace is
	// unfindable in a container's output.
	logged := logw.String()
	for _, want := range []string{"handler panic", "map is nil", "/api/devices/S1", "stack="} {
		if !strings.Contains(logged, want) {
			t.Errorf("log is missing %q: %s", want, logged)
		}
	}
}

// TestRecoverMiddlewareAfterWrite covers the case where the response has already begun:
// appending a 500 to half a document is worse than a truncated one, so the handler stops
// and only the log records the failure.
func TestRecoverMiddlewareAfterWrite(t *testing.T) {
	var logw bytes.Buffer
	h := panicServer(&logw, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("late failure")
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the original 200", rec.Code)
	}
	if body := rec.Body.String(); body != "partial" {
		t.Errorf("body = %q, want the bytes already written and nothing appended", body)
	}
	if !strings.Contains(logw.String(), "late failure") {
		t.Errorf("log is missing the panic: %s", logw.String())
	}
}

// flushRecorder is a writer that both records flushes and can be asserted on.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flushRecorder) Flush() { f.flushes++ }

// TestRecordingWriterKeepsStreaming is the regression guard for the SSE stream:
// handleEvents finds its flusher with a type assertion, so a wrapper that hid
// http.Flusher would not fail visibly — it would turn every live scan into
// "streaming unsupported".
func TestRecordingWriterKeepsStreaming(t *testing.T) {
	var logw bytes.Buffer
	h := panicServer(&logw, func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("handler lost the http.Flusher assertion through the middleware")
		}
		f.Flush()
	})

	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events", nil))

	if rec.flushes != 1 {
		t.Errorf("flushes = %d, want 1 — the wrapper is swallowing Flush", rec.flushes)
	}
}
