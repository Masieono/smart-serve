// Package httpapi serves the JSON API and the SSE stream described in
// docs/API.md. It is deliberately unauthenticated: access control is delegated
// to a reverse proxy (ADR-016).
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/auth"
	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/notify"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/selftest"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// Scheduler is the in-process scan scheduler, when one is running. The API only
// nudges it: a schedule edit takes effect immediately rather than at the next
// sleep boundary, and "run now" goes through the same path as a timed fire.
type Scheduler interface {
	Wake()
	RunNow(ctx context.Context, scheduleID int64) error
}

// Notifier delivers notifications. The API uses it to validate channel configs
// on save and to send test events.
type Notifier interface {
	SendTest(ctx context.Context, ch store.Channel, ev notify.Event) (notify.Outcome, error)
	ValidateConfig(typ string, cfg map[string]any) error
	Types() []string
}

// Options configures the API handler.
type Options struct {
	BasePath string // subpath hosting, e.g. "/smartserve" (ADR-015)
	Version  string
	// Auth, when it carries a complete credential, gates the app behind one user (ADR-033). The
	// zero value leaves the layer off, which is the documented default.
	Auth auth.Credentials
	// AuthIdleMinutes reports the idle window from the settings, so the login layer honours the
	// number the operator set rather than a constant.
	AuthIdleMinutes func(ctx context.Context) int
	// TrustProxy honours `X-Forwarded-Proto` for the session cookie's Secure flag
	// (`SMARTSERVE_TRUST_PROXY`). Off unless the operator says the proxy is there.
	TrustProxy bool
	Logger     *slog.Logger
	// UI, when set, serves the web interface at every path the API does not
	// claim (including SPA deep links).
	UI http.Handler
	// Scheduler, when set, owns scheduled scans. Without it, "run now" falls
	// back to firing a scan directly.
	Scheduler Scheduler
	// Notifier, when set, delivers notifications. Without it, channel configs are
	// not validated and test sends report 501.
	Notifier Notifier
	// CommandTimeout, when set, supplies the per-command timeout for the smartctl calls the API
	// makes itself, the self-test ones. Without it the shipped default is used, which would
	// quietly ignore SMARTSERVE_COMMAND_TIMEOUT for exactly those calls.
	CommandTimeout func(ctx context.Context) time.Duration
	// SelfTests, when set, reports what a scheduled self-test pass is doing. Without it the
	// schedules page simply says nothing about a pass.
	SelfTests SelfTests
	// SelfTestCommands, when set, replaces the smartctl calls the self-test endpoints make. It
	// exists for tests: without it the only endpoints that write to a drive are also the only
	// ones that cannot be exercised. Nil means the real smartctl wrappers.
	SelfTestCommands SelfTestCommands
}

// SelfTests reports on a scheduled self-test pass (`FEATURES.md` A19). *selftest.Runner satisfies it.
//
// Read-only here on purpose. The API can start a self-test on *one* drive, and can report on a pass,
// but starting a pass is a schedule's action rather than a button: a pass writes to every drive in
// the fleet, and the only thing that should be able to do that unattended is a schedule the user
// wrote (`ADR-024`).
type SelfTests interface {
	Progress() selftest.Progress
}

// SelfTestCommands is the three drive operations the self-test endpoints perform.
//
// It exists as an interface for one reason: these are the only endpoints in the API that change a
// drive, and a direct call into the `smart` package cannot be exercised by a test that has no drive
// to change. The production implementation is a thin adapter over the allow-listed `smartctl`
// wrappers, so the seam adds no behaviour of its own, it only lets the endpoint logic (status codes,
// the read-back after an abort, the timeout it passes down) be tested without hardware.
type SelfTestCommands interface {
	Read(ctx context.Context, timeout time.Duration, path, transport string) (smart.Result, error)
	Start(ctx context.Context, timeout time.Duration, path, transport string, kind smart.SelfTestKind) (smart.Result, error)
	Abort(ctx context.Context, timeout time.Duration, path, transport string) (smart.Result, error)
}

// smartCommands is the production reading of SelfTestCommands: smartctl, through the wrappers whose
// arguments are fixed in the package that owns them.
type smartCommands struct{}

func (smartCommands) Read(ctx context.Context, timeout time.Duration, path, transport string) (smart.Result, error) {
	return smart.ReadSelfTestLog(ctx, timeout, path, transport)
}

func (smartCommands) Start(ctx context.Context, timeout time.Duration, path, transport string, kind smart.SelfTestKind) (smart.Result, error) {
	return smart.StartSelfTest(ctx, timeout, path, transport, kind)
}

func (smartCommands) Abort(ctx context.Context, timeout time.Duration, path, transport string) (smart.Result, error) {
	return smart.AbortSelfTest(ctx, timeout, path, transport)
}

// Server is the HTTP API.
type Server struct {
	store     *store.Store
	scanner   *scan.Manager
	hub       *events.Hub
	log       *slog.Logger
	version   string
	scheduler Scheduler
	notifier  Notifier
	selfTests SelfTests
	handler   http.Handler

	// selfTestCmd runs the smartctl self-test commands; never nil after New.
	selfTestCmd SelfTestCommands

	// commandTimeoutFn resolves the per-command timeout for the smartctl calls this package
	// makes directly, so they honour the same setting the scans do.
	commandTimeoutFn func(ctx context.Context) time.Duration

	// auth is the optional single-user gate (ADR-033). Nil means the layer is off.
	auth     *authGate
	basePath string
}

// commandTimeout returns the per-command timeout for the smartctl calls this package makes.
//
// It falls back to the shipped default rather than to zero, because a zero timeout means "no
// limit" to exec: a drive that has stopped answering would then hang the request for as long as
// the kernel allows instead of failing in fifteen seconds.
func (s *Server) commandTimeout(ctx context.Context) time.Duration {
	if s.commandTimeoutFn != nil {
		if v := s.commandTimeoutFn(ctx); v > 0 {
			return v
		}
	}
	return smart.DefaultTimeout
}

// New builds the API handler.
func New(st *store.Store, sc *scan.Manager, hub *events.Hub, opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	selfTestCmd := opts.SelfTestCommands
	if selfTestCmd == nil {
		selfTestCmd = smartCommands{}
	}
	s := &Server{
		store: st, scanner: sc, hub: hub, log: logger, version: opts.Version,
		scheduler: opts.Scheduler, notifier: opts.Notifier, selfTests: opts.SelfTests,
		commandTimeoutFn: opts.CommandTimeout, selfTestCmd: selfTestCmd,
	}

	s.basePath = strings.TrimRight(opts.BasePath, "/")
	if opts.Auth.Enabled() {
		// The window is read per request rather than fixed at start-up, so the settings page's idle
		// timeout takes effect on the next page load instead of needing a restart.
		idle := func() time.Duration {
			if opts.AuthIdleMinutes != nil {
				if mins := opts.AuthIdleMinutes(context.Background()); mins > 0 {
					return time.Duration(mins) * time.Minute
				}
			}
			return auth.DefaultIdle
		}
		s.auth = &authGate{
			base:       s.basePath,
			trustProxy: opts.TrustProxy,
			creds:      opts.Auth,
			store:      auth.New(idle(), auth.DefaultMax),
			idle:       idle,
			log:        logger,
		}
	}

	mux := http.NewServeMux()
	s.routes(mux)
	if opts.UI != nil {
		// Registered last: the API patterns above are more specific and win.
		mux.Handle("/", opts.UI)
	}

	var h http.Handler = mux
	h = sameOriginMiddleware(h)
	// Auth sits inside the security headers (a 401 has to be as constrained as a 200), outside the
	// CSRF guard (an unauthenticated request is answered before the origin is considered), and
	// **inside StripPrefix**, so the paths it judges are the ones the mux serves: `/healthz`,
	// `/login`, `/api/...` rather than `/smartserve/healthz`. Built the other way round the exempt
	// list matches nothing under a subpath, so the healthcheck is redirected, the assets are gated,
	// an API call answers 303 instead of 401, and the login page redirects to itself forever.
	if s.authEnabled() {
		h = s.auth.middleware(h)
	}
	if bp := strings.TrimRight(opts.BasePath, "/"); bp != "" {
		h = http.StripPrefix(bp, h)
	}
	// Outermost first: recovery has to catch a panic from any layer below it, the
	// security headers have to be set before the CSRF guard can reject a request, so
	// that a 403 is as constrained as a 200, and the log line has to see the status the
	// client was actually given, including the 500 the recovery synthesises.
	h = securityHeadersMiddleware(h)
	h = recoverMiddleware(logger, h)
	h = logMiddleware(logger, h)
	s.handler = h
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	// The shell asks this on every load to decide whether to offer a sign-out control, so it answers
	// with `enabled: false` rather than falling through to the SPA when the layer is off.
	mux.HandleFunc("GET /api/session", s.handleSessionInfo)
	if s.authEnabled() {
		mux.HandleFunc("GET /login", s.handleLogin)
		mux.HandleFunc("POST /api/session", s.handleLoginPost)
		mux.HandleFunc("POST /api/logout", s.handleLogout)
	}
	// The Prometheus exporter (`FEATURES.md` C6). Outside `/api` because that is where a scraper
	// looks for it, and behind a setting because enabling it invites someone else's poller.
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	mux.HandleFunc("GET /api/devices", s.handleListDevices)
	// Comparing drives (`FEATURES.md` D6). Aligned server-side because the alignment is the
	// feature, and because it needs every drive's stored attribute blob.
	mux.HandleFunc("GET /api/compare", s.handleCompare)
	// The fleet export (`FEATURES.md` A23). Registered beside the device routes because that is what
	// it exports; a literal segment is more specific than `{serial}`, so the mux picks this one.
	mux.HandleFunc("GET /api/devices/export", s.handleExportFleet)
	mux.HandleFunc("GET /api/devices/{serial}", s.handleGetDevice)
	mux.HandleFunc("DELETE /api/devices/{serial}", s.handleForgetDevice)
	mux.HandleFunc("PUT /api/devices/{serial}/notes", s.handleSetNotes)
	// Acknowledging a condition the user already knows about (`FEATURES.md` B7). It changes no
	// finding, only whether the finding is repeated.
	mux.HandleFunc("PUT /api/devices/{serial}/ack", s.handleAckDevice)
	mux.HandleFunc("DELETE /api/devices/{serial}/ack", s.handleUnackDevice)
	mux.HandleFunc("PUT /api/devices/{serial}/transport", s.handleSetTransport)
	mux.HandleFunc("GET /api/devices/{serial}/raw", s.handleRaw)
	mux.HandleFunc("GET /api/devices/{serial}/report", s.handleReport)
	mux.HandleFunc("GET /api/devices/{serial}/history", s.handleHistory)

	// The self-test endpoints. `GET` reads the drive live rather than from the stored document,
	// because progress cannot come from a document that is only written by a scan; `POST` is the
	// only thing in this API that changes a drive.
	mux.HandleFunc("GET /api/devices/{serial}/selftest", s.handleGetSelfTest)
	mux.HandleFunc("POST /api/devices/{serial}/selftest", s.handleStartSelfTest)
	mux.HandleFunc("POST /api/devices/{serial}/selftest/abort", s.handleAbortSelfTest)

	mux.HandleFunc("POST /api/scan", s.handleScan)
	mux.HandleFunc("POST /api/devices/{serial}/scan", s.handleScanDevice)
	mux.HandleFunc("GET /api/scans", s.handleScanHistory)

	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	// The drive knowledge base (`FEATURES.md` A21). The refresh is a POST because it changes stored
	// state, and it is a *button* rather than a timer: nothing here polls while the app is idle.
	mux.HandleFunc("GET /api/knowledge", s.handleGetKnowledge)
	mux.HandleFunc("POST /api/knowledge/refresh", s.handleRefreshKnowledge)
	mux.HandleFunc("PUT /api/knowledge/overrides", s.handlePutKnowledgeOverrides)
	mux.HandleFunc("DELETE /api/knowledge/cache", s.handleClearKnowledgeCache)

	// Import/export (`FEATURES.md` C7). The export is a download because it is a file to keep; the
	// import takes the same shape back.
	mux.HandleFunc("GET /api/config/export", s.handleExportConfig)
	mux.HandleFunc("POST /api/config/import", s.handleImportConfig)

	mux.HandleFunc("GET /api/schedules", s.handleListSchedules)
	mux.HandleFunc("POST /api/schedules", s.handleCreateSchedule)
	mux.HandleFunc("PUT /api/schedules/{id}", s.handleUpdateSchedule)
	mux.HandleFunc("DELETE /api/schedules/{id}", s.handleDeleteSchedule)
	mux.HandleFunc("POST /api/schedules/{id}/run", s.handleRunSchedule)

	mux.HandleFunc("GET /api/channels", s.handleListChannels)
	mux.HandleFunc("POST /api/channels", s.handleCreateChannel)
	mux.HandleFunc("PUT /api/channels/{id}", s.handleUpdateChannel)
	mux.HandleFunc("DELETE /api/channels/{id}", s.handleDeleteChannel)
	mux.HandleFunc("POST /api/channels/{id}/test", s.handleTestChannel)
	// The body-described variant, used by the editor so Test can prove unsaved edits.
	mux.HandleFunc("POST /api/channels/test", s.handleTestChannelConfig)

	mux.HandleFunc("GET /api/notifications", s.handleNotifications)

	mux.HandleFunc("GET /api/events", s.handleEvents)
}

// sameOriginMiddleware implements the CSRF guard from ADR-015: state-changing
// requests must originate from the same site. Non-browser clients send no Origin
// header and are allowed, so curl and scripts keep working.
func sameOriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				writeError(w, http.StatusForbidden, "cross-site request rejected")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					writeError(w, http.StatusForbidden, "cross-origin request rejected")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// fail logs the error and answers with a reference rather than the error's own words.
//
// A store error can name a column, a file or a SQL statement, internals whose only use
// to a reader is as a hint about what to probe, and whose only use to us is in the log.
// The reference is the bridge between the two: the same four bytes appear in the log
// line, so "it said 3f2a91c4" is a search that finds the failure, its action and its
// stack, instead of a message the reader cannot act on and we would rather not have sent.
//
// This is the 500 path only. Messages that describe the *hardware*, smartctl's own
// reason strings, a drive that will not answer, are the app's product, not its
// internals, and those go out unchanged under their own status.
func (s *Server) fail(w http.ResponseWriter, action string, err error) {
	ref := newRef()
	s.log.Error("request failed", "action", action, "ref", ref, "err", err)
	writeError(w, http.StatusInternalServerError,
		"something went wrong (ref "+ref+"), the detail is in the server log")
}

// --- helpers ------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the body: "+err.Error())
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		writeError(w, http.StatusBadRequest, "empty body: send a JSON object")
		return false
	}
	// Decoding into a struct accepts `null` and leaves every field at zero, which for a full-replace
	// endpoint means "clear everything": `PUT .../notes` with a body of `null` erased a drive's
	// name, notes, tags and bay. A body has to be the shape the handlers describe.
	switch trimmed[0] {
	case '{', '[':
	default:
		writeError(w, http.StatusBadRequest, "body must be a JSON object")
		return false
	}
	// Unmarshal rather than a Decoder, so trailing data after the first value is an error instead of
	// being silently ignored.
	if err := json.Unmarshal(trimmed, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func rawOrNull(s string) json.RawMessage {
	if strings.TrimSpace(s) == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(s)
}
