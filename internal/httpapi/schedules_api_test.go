package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// createSchedule makes one and returns its id, so the tests below say what they are about.
func createSchedule(t *testing.T, srv *Server, body string) int64 {
	t.Helper()
	rec := do(t, srv, http.MethodPost, "/api/schedules", body, nil)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Schedule struct {
			ID int64 `json:"id"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	return out.Schedule.ID
}

// A PUT that renames a schedule must not pause it. Every other field on this handler is optional, so
// `enabled` had to be too: it decoded to false and was assigned unconditionally, which quietly
// stopped monitoring on an edit that was about a label.
func TestPartialScheduleUpdateKeepsItEnabled(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	id := createSchedule(t, srv, `{"enabled":true,"kind":"interval","every_seconds":3600,"label":"nightly"}`)

	rec := do(t, srv, http.MethodPut, "/api/schedules/"+itoa(id), `{"label":"nightly rename"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body = %s", rec.Code, rec.Body.String())
	}

	var out struct {
		Schedule struct {
			Enabled bool   `json:"enabled"`
			Label   string `json:"label"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Schedule.Enabled {
		t.Error("a rename paused the schedule")
	}
	if out.Schedule.Label != "nightly rename" {
		t.Errorf("label = %q, want the new one", out.Schedule.Label)
	}
}

// And a PUT that does say `enabled: false` still pauses it.
func TestScheduleUpdateCanPause(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	id := createSchedule(t, srv, `{"enabled":true,"kind":"interval","every_seconds":3600,"label":"x"}`)

	rec := do(t, srv, http.MethodPut, "/api/schedules/"+itoa(id), `{"enabled":false}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Schedule struct {
			Enabled bool `json:"enabled"`
		} `json:"schedule"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Schedule.Enabled {
		t.Error("enabled:false did not pause it")
	}
}

// A cron expression that cannot be parsed is refused at save time. It used to save with 201 and then
// never fire: the scheduler logged a failed Next on every wake, next_run stayed empty, and the page
// showed an enabled schedule with "n/a" and no explanation.
func TestAnUnreadableCronExpressionIsRejected(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodPost, "/api/schedules",
		`{"enabled":true,"kind":"cron","cron":"every day","label":"broken"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

// A valid expression is accepted, so the check is about parseability rather than about the shape a
// particular client happens to send.
func TestAReadableCronExpressionIsAccepted(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	createSchedule(t, srv, `{"enabled":true,"kind":"cron","cron":"30 3 * * 1","label":"weekly"}`)
}

// "Run now" on a paused schedule used to answer 202 "started", toast "Schedule started", and do
// nothing at all: the update matched no rows because of `AND enabled = 1`, and the store reported
// success regardless.
func TestRunNowOnAPausedScheduleIsRefused(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	id := createSchedule(t, srv, `{"enabled":false,"kind":"interval","every_seconds":3600,"label":"paused"}`)

	rec := do(t, srv, http.MethodPost, "/api/schedules/"+itoa(id)+"/run", "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
}

// itoa keeps the test file free of a strconv import for two call sites.
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
