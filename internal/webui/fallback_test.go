package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An API path that matched no route is a 404, not the SPA with a 200.
//
// The fallback answers every path the API mux did not, which includes `/api/<typo>` and a wrong
// method. Rendering the shell with a 200 told the client the request had worked: `api.js` parsed the
// HTML as null, `res.ok` was true, and the page was left blank with no message, which is exactly the
// failure the client's own comment claims to have fixed.
func TestUnmatchedAPIPathIs404(t *testing.T) {
	h, err := New(themeFunc(Theme{Name: "auto"}), Options{Version: "test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, path := range []string{"/api/devices/", "/api/nonsense", "/api/"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s content-type = %q, want JSON", path, ct)
		}
	}
}

// A page path still gets the shell, which is what makes a deep link work after a reload.
func TestUnknownPageStillGetsTheShell(t *testing.T) {
	h, err := New(themeFunc(Theme{Name: "auto"}), Options{Version: "test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/device/SOMETHING", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the shell", rec.Code)
	}
}
