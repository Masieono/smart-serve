// Package httpapi serves the JSON API and the SSE stream described in
// docs/API.md. It is deliberately unauthenticated: access control is delegated
// to a reverse proxy (ADR-016).
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/notify"
	"github.com/masieono/smart-serve/internal/scan"
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
	Logger   *slog.Logger
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
	// makes itself — the self-test ones. Without it the shipped default is used, which would
	// quietly ignore SMARTSERVE_COMMAND_TIMEOUT for exactly those calls.
	CommandTimeout func(ctx context.Context) time.Duration
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
	handler   http.Handler

	// commandTimeoutFn resolves the per-command timeout for the smartctl calls this package
	// makes directly, so they honour the same setting the scans do.
	commandTimeoutFn func(ctx context.Context) time.Duration
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
	s := &Server{
		store: st, scanner: sc, hub: hub, log: logger, version: opts.Version,
		scheduler: opts.Scheduler, notifier: opts.Notifier,
		commandTimeoutFn: opts.CommandTimeout,
	}

	mux := http.NewServeMux()
	s.routes(mux)
	if opts.UI != nil {
		// Registered last: the API patterns above are more specific and win.
		mux.Handle("/", opts.UI)
	}

	var h http.Handler = mux
	h = sameOriginMiddleware(h)
	if bp := strings.TrimRight(opts.BasePath, "/"); bp != "" {
		h = http.StripPrefix(bp, h)
	}
	s.handler = h
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	mux.HandleFunc("GET /api/devices", s.handleListDevices)
	// The fleet export (`FEATURES.md` A23). Registered beside the device routes because that is what
	// it exports; a literal segment is more specific than `{serial}`, so the mux picks this one.
	mux.HandleFunc("GET /api/devices/export", s.handleExportFleet)
	mux.HandleFunc("GET /api/devices/{serial}", s.handleGetDevice)
	mux.HandleFunc("DELETE /api/devices/{serial}", s.handleForgetDevice)
	mux.HandleFunc("PUT /api/devices/{serial}/notes", s.handleSetNotes)
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

func (s *Server) fail(w http.ResponseWriter, action string, err error) {
	s.log.Error("request failed", "action", action, "err", err)
	writeError(w, http.StatusInternalServerError, err.Error())
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
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
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
