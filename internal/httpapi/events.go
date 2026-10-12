package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/scan"
)

// handleScan triggers a scan through the single-flight manager. By default it
// returns immediately (202) and progress arrives over SSE; ?wait=1 blocks until
// the scan finishes, which is convenient for scripts and tests.
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	trigger := scan.Manual
	if kind := r.URL.Query().Get("trigger"); kind != "" {
		trigger = scan.Trigger{Kind: kind}
	}

	if r.URL.Query().Get("wait") == "1" {
		res, joined, err := s.scanner.Scan(r.Context(), trigger)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"scan_id": res.ScanID, "status": "failed", "error": err.Error(),
			})
			return
		}
		status := "completed"
		if joined {
			status = "joined"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"scan_id": res.ScanID, "status": status, "devices_total": len(res.Devices),
		})
		return
	}

	status := "started"
	if s.scanner.Running() {
		status = "joined"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, _, _ = s.scanner.Scan(ctx, trigger)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status})
}

func (s *Server) handleScanHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	history, err := s.store.ListScanHistory(r.Context(), limit)
	if err != nil {
		s.fail(w, "scan history", err)
		return
	}
	out := make([]map[string]any, 0, len(history))
	for _, h := range history {
		out = append(out, map[string]any{
			"id":             h.ID,
			"started_at":     rfc3339(h.StartedAt),
			"ended_at":       rfc3339(h.EndedAt),
			"trigger":        h.Trigger,
			"ok":             h.OK,
			"error":          h.Error,
			"devices_total":  h.DevicesTotal,
			"devices_ok":     h.DevicesOK,
			"devices_failed": h.DevicesFailed,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"scans": out})
}

// handleEvents is the SSE stream (docs/API.md, ADR-006).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // tell nginx not to buffer (ADR-015)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Replay anything the client missed, then follow live.
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if id, err := strconv.ParseInt(last, 10, 64); err == nil {
			for _, ev := range s.hub.Since(id) {
				writeSSE(w, ev)
			}
			flusher.Flush()
		}
	}

	id, ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(id)

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// A comment keeps idle proxies from closing the stream.
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			writeSSE(w, ev)
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev events.Event) {
	payload, err := json.Marshal(ev.Data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, payload)
}
