// Package httpapi serves the JSON API and the SSE stream described in
// docs/API.md. It is deliberately unauthenticated: access control is delegated
// to a reverse proxy (ADR-016).
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/store"
)

// Options configures the API handler.
type Options struct {
	BasePath string // subpath hosting, e.g. "/smartserve" (ADR-015)
	Version  string
	Logger   *slog.Logger
	// UI, when set, serves the web interface at every path the API does not
	// claim (including SPA deep links).
	UI http.Handler
}

// Server is the HTTP API.
type Server struct {
	store   *store.Store
	scanner *scan.Manager
	hub     *events.Hub
	log     *slog.Logger
	version string
	handler http.Handler
}

// New builds the API handler.
func New(st *store.Store, sc *scan.Manager, hub *events.Hub, opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: st, scanner: sc, hub: hub, log: logger, version: opts.Version}

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
	mux.HandleFunc("GET /api/devices/{serial}", s.handleGetDevice)
	mux.HandleFunc("PUT /api/devices/{serial}/notes", s.handleSetNotes)
	mux.HandleFunc("PUT /api/devices/{serial}/transport", s.handleSetTransport)
	mux.HandleFunc("GET /api/devices/{serial}/raw", s.handleRaw)
	mux.HandleFunc("GET /api/devices/{serial}/report", s.handleReport)
	mux.HandleFunc("GET /api/devices/{serial}/history", s.handleHistory)

	mux.HandleFunc("POST /api/scan", s.handleScan)
	mux.HandleFunc("GET /api/scans", s.handleScanHistory)

	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

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
