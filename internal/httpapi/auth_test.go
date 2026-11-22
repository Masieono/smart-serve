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

// A subpath deployment, which is the case the first build got wrong.
//
// The gate has to sit *inside* StripPrefix, so the paths it judges are the ones the mux serves.
// Built the other way round, every exempt path stopped matching: the container healthcheck was
// redirected, the SPA's assets were gated, an API call answered 303 instead of 401, and the login
// page redirected to itself, which made the app unusable in a browser and left only Basic working.
func TestBasePathDeploymentKeepsTheGateOnStrippedPaths(t *testing.T) {
	s, _, _, _ := newTestServer(t, Options{
		Auth:     auth.Credentials{User: "admin", Password: "s3cret"},
		BasePath: "/smartserve",
		UI:       http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "app") }),
	})

	for _, c := range []struct {
		path string
		want int
		why  string
	}{
		{"/smartserve/healthz", http.StatusOK, "a container healthcheck cannot sign in"},
		{"/smartserve/login", http.StatusOK, "the login page cannot require a login"},
		{"/smartserve/js/app.js", http.StatusOK, "the login page needs its own assets"},
		{"/smartserve/api/devices", http.StatusUnauthorized, "an API call is answered JSON, not a redirect"},
		{"/smartserve/schedules", http.StatusSeeOther, "a page goes to the login page"},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("GET %s = %d, want %d (%s)", c.path, rec.Code, c.want, c.why)
		}
	}

	// Every Location the gate builds has to stay under the subpath, or the browser leaves the app.
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/smartserve/schedules", nil))
	if got := rec.Header().Get("Location"); got != "/smartserve/login?next=%2Fschedules" {
		t.Errorf("Location = %q, want the prefixed login page carrying the stripped path", got)
	}
}

// The whole round trip under a subpath: sign in, follow the redirect, reach the page, sign out.
func TestSubpathSignInRoundTrip(t *testing.T) {
	s, _, _, _ := newTestServer(t, Options{
		Auth:     auth.Credentials{User: "admin", Password: "s3cret"},
		BasePath: "/smartserve",
		UI:       http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "app") }),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/smartserve/api/session",
		strings.NewReader("user=admin&password=s3cret&next=/schedules"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign-in = %d, want 303; body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/smartserve/schedules" {
		t.Errorf("Location = %q, want /smartserve/schedules", got)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie")
	}
	if cookies[0].Path != "/smartserve" {
		t.Errorf("cookie Path = %q, want /smartserve", cookies[0].Path)
	}

	// With the cookie, the page behind the subpath opens.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/smartserve/schedules", nil)
	req.AddCookie(cookies[0])
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "app" {
		t.Fatalf("signed-in page = %d %q, want 200 app", rec.Code, rec.Body.String())
	}

	// And signing out stays under the subpath too.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/smartserve/api/logout", nil)
	req.AddCookie(cookies[0])
	s.ServeHTTP(rec, req)
	if got := rec.Header().Get("Location"); got != "/smartserve/login" {
		t.Errorf("logout Location = %q, want /smartserve/login", got)
	}
}
