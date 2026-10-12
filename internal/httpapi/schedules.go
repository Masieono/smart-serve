package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/store"
)

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// --- settings -----------------------------------------------------------------

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, "get settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.store.SetSettings(r.Context(), body); err != nil {
		s.fail(w, "put settings", err)
		return
	}
	// A changed timezone or missed-run policy has to re-arm the scheduler, or it
	// would keep using the old values until the process restarted.
	s.wakeScheduler()
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, "get settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

// --- schedules ----------------------------------------------------------------

type scheduleJSON struct {
	ID           int64   `json:"id"`
	Enabled      bool    `json:"enabled"`
	Kind         string  `json:"kind"`
	EverySeconds *int64  `json:"every_seconds,omitempty"`
	Cron         string  `json:"cron,omitempty"`
	Label        string  `json:"label"`
	LastRun      *string `json:"last_run,omitempty"`
	NextRun      *string `json:"next_run,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

func toScheduleJSON(sched store.Schedule) scheduleJSON {
	out := scheduleJSON{
		ID: sched.ID, Enabled: sched.Enabled, Kind: sched.Kind,
		EverySeconds: sched.EverySeconds, Cron: sched.Cron, Label: sched.Label,
		CreatedAt: rfc3339(sched.CreatedAt),
	}
	if sched.LastRun != nil {
		v := rfc3339(*sched.LastRun)
		out.LastRun = &v
	}
	if sched.NextRun != nil {
		v := rfc3339(*sched.NextRun)
		out.NextRun = &v
	}
	return out
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := s.store.ListSchedules(r.Context())
	if err != nil {
		s.fail(w, "list schedules", err)
		return
	}
	out := make([]scheduleJSON, 0, len(schedules))
	for _, sched := range schedules {
		out = append(out, toScheduleJSON(sched))
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": out})
}

func (s *Server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	var body scheduleJSON
	if !decodeJSON(w, r, &body) {
		return
	}
	if msg := validateSchedule(body.Kind, body.EverySeconds, body.Cron); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	id, err := s.store.CreateSchedule(r.Context(), store.Schedule{
		Enabled: body.Enabled, Kind: body.Kind, EverySeconds: body.EverySeconds,
		Cron: body.Cron, Label: body.Label,
	})
	if err != nil {
		s.fail(w, "create schedule", err)
		return
	}
	sched, err := s.store.GetSchedule(r.Context(), id)
	if err != nil || sched == nil {
		s.fail(w, "create schedule", err)
		return
	}
	s.wakeScheduler()
	writeJSON(w, http.StatusCreated, map[string]any{"schedule": toScheduleJSON(*sched)})
}

func (s *Server) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	existing, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		s.fail(w, "update schedule", err)
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "unknown schedule")
		return
	}

	var body scheduleJSON
	if !decodeJSON(w, r, &body) {
		return
	}
	existing.Enabled = body.Enabled
	if body.Kind != "" {
		existing.Kind = body.Kind
	}
	if body.EverySeconds != nil {
		existing.EverySeconds = body.EverySeconds
	}
	if body.Cron != "" {
		existing.Cron = body.Cron
	}
	if body.Label != "" {
		existing.Label = body.Label
	}
	if msg := validateSchedule(existing.Kind, existing.EverySeconds, existing.Cron); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateSchedule(r.Context(), *existing); err != nil {
		s.fail(w, "update schedule", err)
		return
	}
	s.wakeScheduler()
	sched, _ := s.store.GetSchedule(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"schedule": toScheduleJSON(*sched)})
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSchedule(r.Context(), id); err != nil {
		s.fail(w, "delete schedule", err)
		return
	}
	s.wakeScheduler()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// wakeScheduler nudges the in-process scheduler so a schedule change takes
// effect immediately instead of at the next sleep boundary.
func (s *Server) wakeScheduler() {
	if s.scheduler != nil {
		s.scheduler.Wake()
	}
}

// handleRunSchedule fires a scan now for a schedule. With the in-process
// scheduler running, "run now" brings the schedule's next run forward and wakes
// the loop, so last-run/next-run are recorded exactly as for a timed fire.
func (s *Server) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	sched, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		s.fail(w, "run schedule", err)
		return
	}
	if sched == nil {
		writeError(w, http.StatusNotFound, "unknown schedule")
		return
	}

	if s.scheduler != nil {
		if err := s.scheduler.RunNow(r.Context(), id); err != nil {
			s.fail(w, "run schedule", err)
			return
		}
		// The scheduler publishes schedule_fired itself when it actually runs.
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "started", "schedule_id": id})
		return
	}

	// Without a scheduler (tests, or the scan-only build) run it directly.
	s.hub.Publish("schedule_fired", map[string]any{"schedule_id": id})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, _, _ = s.scanner.Scan(ctx, scan.Trigger{Kind: "schedule", ScheduleID: id})
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "started", "schedule_id": id})
}

func validateSchedule(kind string, every *int64, cron string) string {
	switch kind {
	case "interval":
		if every == nil || *every <= 0 {
			return "every_seconds is required for interval schedules"
		}
	case "cron":
		if strings.TrimSpace(cron) == "" {
			return "cron is required for cron schedules"
		}
	default:
		return "kind must be interval or cron"
	}
	return ""
}
