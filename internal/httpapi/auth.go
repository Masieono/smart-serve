// Optional single-user authentication (ADR-033).
//
// Two ways in, one credential:
//
//   - A browser posts the login form, gets an HttpOnly session cookie, and logs out by dropping it.
//   - Everything else (curl, scripts, Prometheus) sends HTTP Basic and never sees a session at all.
//
// The second is not a compatibility shim: it is why the layer can be switched on in a homelab
// without breaking the scraper, the backup script or the healthcheck.
package httpapi

import (
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/auth"
)

const (
	sessionCookie = "smartserve_session"
	// Where the login form sends a browser after a successful post when it has no better idea.
	loginPath = "/login"
)

// authGate is the middleware and the handlers that go with it.
type authGate struct {
	base  string // the deployment's base path, for Location headers the browser will follow
	creds auth.Credentials
	store *auth.Store
	// idle is read from the settings on demand, because the idle window is a setting and a session
	// issued before it changed has to be judged by the new value.
	idle func() time.Duration
	log  interface{ Warn(string, ...any) }
}

// exempt reports whether a path is served without a session.
//
// The healthcheck is exempt because a container probe cannot carry a secret, and the login page
// exempt because it is how a session is obtained. Static assets carry no data.
func exempt(r *http.Request) bool {
	path := r.URL.Path
	switch {
	// The login endpoint itself: it is how a session is obtained, so it cannot require one.
	case path == "/api/session" && r.Method == http.MethodPost:
		return true
	case path == "/healthz":
		return true
	case path == loginPath:
		return true
	case path == "/icon.svg", path == "/favicon.ico":
		return true
	case strings.HasPrefix(path, "/styles/"):
		return true
	case strings.HasPrefix(path, "/js/"):
		return true
	}
	return false
}

func isAPIPath(path string) bool { return strings.HasPrefix(path, "/api/") }

// middleware is the gate itself. It authenticates, and on failure answers in the shape the client
// asked for: JSON for the API, a redirect to the login page for a browser.
func (g *authGate) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exempt(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Basic first: a script that sends it must never be bounced to a login page.
		if user, pass, ok := r.BasicAuth(); ok && g.creds.Match(user, pass) {
			next.ServeHTTP(w, r)
			return
		}

		if c, err := r.Cookie(sessionCookie); err == nil && g.touch(c.Value) {
			next.ServeHTTP(w, r)
			return
		}

		if isAPIPath(r.URL.Path) || strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("WWW-Authenticate", `Basic realm="Smart-Serve", charset="UTF-8"`)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication required"})
			return
		}

		// A browser: remember where it was going, so logging in lands there rather than at the root.
		next := r.URL.RequestURI()
		if next == "" || next == "/" {
			next = ""
		}
		target := joinBase(g.base, loginPath)
		if next != "" {
			target += "?next=" + template.URLQueryEscaper(next)
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

// touch refreshes a session against the window the settings currently hold.
//
// `idle` is a function, not a value: the operator can shorten or lengthen the timeout from the
// settings page, and a store built with a fixed duration would keep the old one until a restart.
func (g *authGate) touch(token string) bool {
	if want := g.idle(); want > 0 && want != g.store.Idle() {
		g.store.SetIdle(want)
	}
	return g.store.Touch(token)
}

// handleSessionInfo tells the shell whether the layer is on and who is asking.
func (s *Server) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	authed := false
	if c, err := r.Cookie(sessionCookie); err == nil && s.auth != nil {
		authed = s.auth.store.Touch(c.Value)
	}
	if user, pass, ok := r.BasicAuth(); ok && s.auth != nil && s.auth.creds.Match(user, pass) {
		authed = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       s.auth != nil,
		"authenticated": !s.authEnabled() || authed,
	})
}

// handleLoginPost verifies the form and issues a session.
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	user := r.PostFormValue("user")
	pass := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	if !s.auth.creds.Match(user, pass) {
		// Deliberately vague and deliberately slow-ish in spirit: one message for "no such user" and
		// "wrong password", because the distinction is only useful to somebody guessing.
		s.log.Warn("login refused", "remote", remoteHost(r))
		s.renderLogin(w, r, http.StatusUnauthorized, "That username and password did not match.", next)
		return
	}

	token, err := s.auth.store.Issue()
	if err != nil {
		s.fail(w, "issue a session", err)
		return
	}
	s.log.Info("login", "remote", remoteHost(r))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     s.basePathOrRoot(),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	})
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, s.link(next), http.StatusSeeOther)
}

// handleLogout ends the session server-side and clears the cookie.
//
// Revoking matters as much as clearing: a cookie that is only dropped still works for anyone who
// copied it, which is the difference between logging out and hiding the button.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.auth.store.Revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     s.basePathOrRoot(),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   -1,
	})
	http.Redirect(w, r, s.link(loginPath), http.StatusSeeOther)
}

// handleLogin serves the form.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && s.auth.store.Touch(c.Value) {
		http.Redirect(w, r, s.link("/"), http.StatusSeeOther)
		return
	}
	s.renderLogin(w, r, http.StatusOK, "", safeNext(r.URL.Query().Get("next")))
}

func (s *Server) renderLogin(w http.ResponseWriter, r *http.Request, status int, message, next string) {
	data := struct {
		Message string
		Next    string
		Base    string
	}{Message: message, Next: next, Base: s.baseHref()}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = loginTemplate.Execute(w, data)
}

// safeNext keeps a redirect target on this site: a login page that forwards to another origin is an
// open redirect, and one that forwards to `//evil` is the same thing wearing a path.
func safeNext(next string) string {
	if next == "" {
		return ""
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return ""
	}
	return next
}

// basePathOrRoot is the prefix a cookie is scoped to: the subpath when there is one, else `/`.
func (s *Server) basePathOrRoot() string { return rootOf(s.basePath) }

// baseHref is the same prefix with a trailing slash, for joining a relative URL onto it.
func (s *Server) baseHref() string { return hrefOf(s.basePath) }

// link puts a server-relative path back under the base path.
func (s *Server) link(p string) string { return joinBase(s.basePath, p) }

func rootOf(base string) string {
	if base == "" {
		return "/"
	}
	return base
}

func hrefOf(base string) string {
	if base == "" {
		return "/"
	}
	return base + "/"
}

// joinBase puts a server-relative path back under the base path.
//
// The gate and the handlers work in stripped paths (`/login`, `/api/devices`), because that is what
// the mux serves, but a Location header is read by the browser and has to include the subpath. It is
// idempotent: a `next` that already carries the prefix is left alone rather than prefixed twice.
func joinBase(base, p string) string {
	if base == "" || p == "" {
		return p
	}
	if p == base || strings.HasPrefix(p, base+"/") || strings.HasPrefix(p, base+"?") {
		return p
	}
	return base + p
}

func (s *Server) authEnabled() bool { return s.auth != nil }

// remoteHost is the client address for a log line, without the port.
func remoteHost(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sign in · Smart-Serve</title>
<style>
  :root { color-scheme: dark light; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center;
         font: 15px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif;
         background: #0d1117; color: #e6edf3; }
  @media (prefers-color-scheme: light) { body { background: #f6f8fa; color: #1f2328; } }
  form { width: min(360px, 90vw); display: flex; flex-direction: column; gap: 14px; }
  h1 { margin: 0 0 4px; font-size: 18px; font-weight: 600; }
  label { display: flex; flex-direction: column; gap: 6px; font-size: 12px; opacity: 0.75; }
  input { height: 38px; padding: 0 12px; font-size: 14px; border-radius: 6px;
          border: 1px solid #30363d; background: #161b22; color: inherit; }
  @media (prefers-color-scheme: light) { input { border-color: #d8dee4; background: #fff; } }
  button { height: 38px; border: 0; border-radius: 6px; background: #3b82f6; color: #fff;
           font-size: 14px; font-weight: 500; cursor: pointer; }
  .err { color: #f85149; font-size: 13px; margin: 0; }
</style></head>
<body><form method="post" action="{{.Base}}api/session">
  <h1>Smart-Serve</h1>
  {{if .Message}}<p class="err">{{.Message}}</p>{{end}}
  <input type="hidden" name="next" value="{{.Next}}">
  <label>Username<input name="user" autocomplete="username" autofocus required></label>
  <label>Password<input name="password" type="password" autocomplete="current-password" required></label>
  <button type="submit">Sign in</button>
</form></body></html>`))
