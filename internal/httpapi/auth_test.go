package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/auth"
)

func gatedServer(t *testing.T) *Server {
	t.Helper()
	s, _, _, _ := newTestServer(t, Options{
		Auth: auth.Credentials{User: "admin", Password: "s3cret"},
		UI:   http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "app") }),
	})
	return s
}

func TestAuthOffLeavesEverythingOpen(t *testing.T) {
	s, _, _, _ := newTestServer(t, Options{})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("with auth off, the API answers normally; got %d", rec.Code)
	}
}

func TestAPIIs401WithoutACredential(t *testing.T) {
	s := gatedServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/devices", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "authentication required") {
		t.Errorf("body = %q, want a JSON error", rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want a Basic challenge for scripts", got)
	}
}

func TestAPageRedirectsToTheLoginForm(t *testing.T) {
	s := gatedServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schedules", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Errorf("Location = %q, want the login page", loc)
	}
}

func TestHealthzStaysExempt(t *testing.T) {
	s := gatedServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("a container probe cannot carry a secret; got %d", rec.Code)
	}
}

func TestBasicAuthPassesForScripts(t *testing.T) {
	s := gatedServer(t)
	for _, path := range []string{"/api/devices", "/schedules"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetBasicAuth("admin", "s3cret")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code >= 400 {
			t.Errorf("%s with Basic got %d, want it served", path, rec.Code)
		}
	}
}

func TestWrongPasswordIsRefused(t *testing.T) {
	s := gatedServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginIssuesASessionThatOutlivesTheForm(t *testing.T) {
	s := gatedServer(t)

	form := url.Values{"user": {"admin"}, "password": {"s3cret"}, "next": {"/schedules"}}
	req := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect after a successful login (%s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/schedules" {
		t.Errorf("Location = %q, want the page the reader asked for", loc)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != sessionCookie {
		t.Fatalf("no session cookie in %v", cookies)
	}
	if !cookies[0].HttpOnly {
		t.Error("the session cookie must be HttpOnly: it is not for scripts to read")
	}
	if cookies[0].SameSite != http.SameSiteStrictMode {
		t.Error("the session cookie must be SameSite=Strict")
	}

	req = httptest.NewRequest(http.MethodGet, "/schedules", nil)
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "app" {
		t.Fatalf("with the session cookie the page should be served, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestLogoutRevokesServerSide(t *testing.T) {
	s := gatedServer(t)

	form := url.Values{"user": {"admin"}, "password": {"s3cret"}}
	req := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	req = httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want a redirect", rec.Code)
	}

	// The cookie value is now dead everywhere, not merely cleared in this browser: that is the
	// difference between logging out and hiding the button.
	req = httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked token still worked: %d", rec.Code)
	}
	if s.auth.store.Count() != 0 {
		t.Errorf("expected no live sessions, got %d", s.auth.store.Count())
	}
}

func TestLoginRefusesAndExplainsNothing(t *testing.T) {
	s := gatedServer(t)
	form := url.Values{"user": {"admin"}, "password": {"nope"}}
	req := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "no such user") || strings.Contains(body, "unknown user") {
		t.Error("the form must not say which half was wrong")
	}
	if s.auth.store.Count() != 0 {
		t.Error("a failed login must not leave a session behind")
	}
}

func TestOffsiteNextIsIgnored(t *testing.T) {
	// An open redirect on the login page would let a phishing link bounce through this app.
	for _, next := range []string{"https://evil.example", "//evil.example", "/\\evil.example"} {
		if got := safeNext(next); got != "" {
			t.Errorf("safeNext(%q) = %q, want empty", next, got)
		}
	}
	if got := safeNext("/schedules"); got != "/schedules" {
		t.Errorf("a same-site path must survive, got %q", got)
	}
}

func TestSessionInfoReportsTheLayersState(t *testing.T) {
	s := gatedServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.SetBasicAuth("admin", "s3cret")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"authenticated":true`) {
		t.Errorf("body = %s", body)
	}
}
