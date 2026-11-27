package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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

	// A full scan can only join a full scan. Saying "started" and spawning a goroutine that will
	// come straight back with ErrBusy is the same lie as reporting another drive's scan as this one.
	running, coversFull := s.scanner.Busy()
	if running && !coversFull {
		writeError(w, http.StatusConflict,
			"another scan is in progress and it is reading a single drive; a full scan cannot join it. "+
				"Try again when it finishes.")
		return
	}
	status := "started"
	if running {
		status = "joined"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, _, _ = s.scanner.Scan(ctx, trigger)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status})
}

// handleScanDevice rescans a single drive.
//
// What makes the per-drive Scan button honest: it reads one drive, not the fleet (docs/API.md).
func (s *Server) handleScanDevice(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	if serial == "" {
		writeError(w, http.StatusBadRequest, "serial is required")
		return
	}

	// The stored record supplies the drive's last-seen path, which is how a bridge
	// that reports a different serial to lsblk than the drive does to smartctl is
	// found again.
	path := ""
	if rec, derr := s.store.GetDevice(r.Context(), serial); derr == nil && rec != nil {
		path = rec.DevicePath
	}

	res, joined, err := s.scanner.ScanDevice(r.Context(), serial, path, scan.DeviceScan)
	if err != nil {
		if errors.Is(err, scan.ErrDeviceNotConnected) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, scan.ErrBusy) {
			// The honest answer: nothing was read, and the UI must not say it was.
			writeError(w, http.StatusConflict,
				"another scan is in progress; try again when it finishes.")
			return
		}
		// The failure itself is not always about the drive: it can come from the store
		// write at the end of the pipeline. The drive's own reason string reaches the
		// card through the stored snapshot (`smart_error`), so nothing useful is lost by
		// keeping the database's words out of this response.
		s.fail(w, "scan device", err)
		return
	}

	status := "completed"
	if joined {
		status = "joined"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scan_id":       res.ScanID,
		"status":        status,
		"serial":        serial,
		"devices_total": len(res.Devices),
	})
}

// handleForgetDevice removes a drive and everything derived from it. This is the
// escape hatch for a workflow that plugs in drives to read them once: without it the
// list fills up with hardware that will never come back (docs/API.md).
func (s *Server) handleForgetDevice(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	if serial == "" {
		writeError(w, http.StatusBadRequest, "serial is required")
		return
	}

	found, err := s.store.DeleteDevice(r.Context(), serial)
	if err != nil {
		s.fail(w, "forget device", err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no such device")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"serial": serial, "forgotten": true})
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

	// Subscribe *first*, then replay.
	//
	// The other order had a hole: anything published between reading the ring and subscribing was in
	// neither, and the client's Last-Event-ID had already moved past it. A lost `scan_complete` left
	// the fleet showing a scan in progress forever.
	id, ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(id)

	// One writer, and it drops an id it has already sent, so an event that was published in the
	// moment between subscribing and replaying is written once rather than twice.
	var highest int64
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if n, err := strconv.ParseInt(last, 10, 64); err == nil {
			highest = n
		}
	}
	// A peer that stops reading without closing (a laptop that slept, wifi that dropped) fills the
	// socket buffer and parks this handler inside Write, where it never reaches the select on
	// `r.Context().Done()`. One such peer then defeats the clean-stop wiring: `hub.Close` cannot
	// unblock a goroutine stuck in a write, `srv.Shutdown` waits out its whole timeout, and the
	// process exits non-zero, which is the symptom that wiring exists to prevent. The deadline turns
	// a stuck write into a return, and the browser reconnects on its own.
	rc := http.NewResponseController(w)
	const writeDeadline = 10 * time.Second
	write := func(b []byte) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(writeDeadline))
		if _, err := w.Write(b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	send := func(ev events.Event) bool {
		if ev.ID <= highest {
			return true
		}
		payload, err := json.Marshal(ev.Data)
		if err != nil {
			return true
		}
		if !write([]byte(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, payload))) {
			return false
		}
		highest = ev.ID
		return true
	}
	for _, ev := range s.hub.Since(highest) {
		if !send(ev) {
			return
		}
	}

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// A comment keeps idle proxies from closing the stream.
			if !write([]byte(": keep-alive\n\n")) {
				return
			}
		case ev, open := <-ch:
			if !open {
				// The hub closed the channel: shutdown, or this subscriber fell too far behind. The
				// browser reconnects on its own and replays from `Last-Event-ID`.
				return
			}
			if !send(ev) {
				return
			}
		}
	}
}
