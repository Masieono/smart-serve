package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/store"
)

// "All" means everything this install kept.
//
// The history endpoint defaulted `from` to a fixed 180 days, so a `retention_days` of 365 showed six
// months behind a range picker that said "All": the one setting whose purpose is to keep more
// history was ignored by the endpoint that reads it, and the truncation was invisible.
func TestHistoryDefaultFollowsRetention(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	ctx := context.Background()

	if err := st.SetSettings(ctx, map[string]string{"retention_days": "365"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}

	now := time.Now()
	old := now.AddDate(0, 0, -300) // well past the old 180-day default, inside a year
	for _, at := range []time.Time{old, now} {
		if _, err := st.SaveScan(ctx, "manual", at, at,
			[]store.ScannedDevice{{Serial: "H1", Health: "good", Score: 100}}); err != nil {
			t.Fatalf("SaveScan: %v", err)
		}
	}

	rec := do(t, srv, http.MethodGet, "/api/devices/H1/history", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !contains(body, "\"ts\"") {
		t.Fatalf("no series in the response: %s", body)
	}
	// Two points, because a 365-day retention must not be cut at 180.
	if n := countOccurrences(body, "\"ts\""); n < 2 {
		t.Errorf("points = %d, want the 300-day-old scan as well: the window is still fixed at 180 days", n)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func countOccurrences(haystack, needle string) int {
	n := 0
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			n++
		}
	}
	return n
}
