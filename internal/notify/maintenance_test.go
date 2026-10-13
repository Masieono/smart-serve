package notify

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/store"
)

func ms(t time.Time) int64 { return t.UnixMilli() }

// window builds the JSON the maintenance_windows setting holds.
func window(label string, start, end time.Time, serials ...string) string {
	quoted := ""
	for i, s := range serials {
		if i > 0 {
			quoted += ","
		}
		quoted += fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf(`{"label":%q,"starts_at":%d,"ends_at":%d,"serials":[%s]}`,
		label, ms(start), ms(end), quoted)
}

// --- parsing ------------------------------------------------------------------

func TestParseMaintenanceWindows(t *testing.T) {
	start := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)

	t.Run("reads a well-formed window", func(t *testing.T) {
		got := ParseMaintenanceWindows("[" + window("array rebuild", start, end, "S1") + "]")
		if len(got) != 1 {
			t.Fatalf("windows = %v, want one", got)
		}
		if got[0].Label != "array rebuild" || got[0].StartsAt != ms(start) || got[0].EndsAt != ms(end) {
			t.Errorf("window = %+v", got[0])
		}
		if len(got[0].Serials) != 1 || got[0].Serials[0] != "S1" {
			t.Errorf("serials = %v", got[0].Serials)
		}
	})

	// A malformed setting must not silence anything: failing closed here would let a
	// typo stop every alert on the host.
	t.Run("tolerates anything malformed", func(t *testing.T) {
		for _, raw := range []string{
			"", "   ", "not json", `{"label":"an object not a list"}`, "null", "[]",
			// A window ending before it starts would never apply; one with no end
			// would never expire.
			"[" + window("backwards", end, start) + "]",
			`[{"starts_at":0,"ends_at":0}]`,
		} {
			if got := ParseMaintenanceWindows(raw); len(got) != 0 {
				t.Errorf("ParseMaintenanceWindows(%q) = %v, want none", raw, got)
			}
		}
	})

	t.Run("keeps the good windows and drops the bad ones", func(t *testing.T) {
		raw := "[" + window("good", start, end) + "," + window("bad", end, start) + "]"
		got := ParseMaintenanceWindows(raw)
		if len(got) != 1 || got[0].Label != "good" {
			t.Errorf("windows = %v, want only the well-formed one", got)
		}
	})
}

// --- matching -----------------------------------------------------------------

func TestActiveWindow(t *testing.T) {
	start := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	windows := ParseMaintenanceWindows("[" + window("rebuild", start, end) + "]")

	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before the window", start.Add(-time.Second), false},
		{"at the start", start, true},
		{"in the middle", start.Add(2 * time.Hour), true},
		// The end is exclusive, so a window ending at 06:00 does not cover 06:00.
		{"at the end", end, false},
		{"after the window", end.Add(time.Second), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ActiveWindow(windows, c.at, "S1", time.UTC) != nil; got != c.want {
				t.Errorf("ActiveWindow = %v, want %v", got, c.want)
			}
		})
	}

	if ActiveWindow(nil, start, "S1", time.UTC) != nil {
		t.Error("no windows means no silence")
	}
}

// An empty serial list covers every drive; a populated one covers only those.
func TestActiveWindowScopesToSerials(t *testing.T) {
	start := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	inside := start.Add(30 * time.Minute)

	all := ParseMaintenanceWindows("[" + window("everywhere", start, end) + "]")
	if ActiveWindow(all, inside, "anything", time.UTC) == nil {
		t.Error("a window with no serials should cover every drive")
	}

	scoped := ParseMaintenanceWindows("[" + window("sda only", start, end, "S1") + "]")
	if ActiveWindow(scoped, inside, "S1", time.UTC) == nil {
		t.Error("the listed drive should be covered")
	}
	if ActiveWindow(scoped, inside, "S2", time.UTC) != nil {
		t.Error("an unlisted drive must not be silenced")
	}
	// Serial numbers are matched case-insensitively because they are hand-typed.
	if ActiveWindow(scoped, inside, "s1", time.UTC) == nil {
		t.Error("serial matching should ignore case")
	}
	// An event with no serial (a scan failure) is not covered by a scoped window.
	if ActiveWindow(scoped, inside, "", time.UTC) != nil {
		t.Error("a scoped window must not silence an event with no drive")
	}
}

// A narrower window added after a broad one must be able to carve out an exception.
func TestActiveWindowPrefersTheLastMatch(t *testing.T) {
	start := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	end := start.Add(6 * time.Hour)
	raw := "[" + window("whole host", start, end) + "," + window("boot drive", start.Add(time.Hour), end) + "]"

	w := ActiveWindow(ParseMaintenanceWindows(raw), start.Add(2*time.Hour), "S1", time.UTC)
	if w == nil || w.Label != "boot drive" {
		t.Errorf("window = %v, want the later one to win", w)
	}
}

func TestMaintenanceWindowName(t *testing.T) {
	if got := (MaintenanceWindow{Label: "  rebuild  "}).Name(); got != "rebuild" {
		t.Errorf("Name = %q, want the trimmed label", got)
	}
	if got := (MaintenanceWindow{}).Name(); got != "maintenance window" {
		t.Errorf("Name = %q, want a fallback", got)
	}
}

// --- recurrence ---------------------------------------------------------------

// recurring builds a daily/weekly window at a time of day.
func recurring(label, recurrence string, startMin, endMin int, days []int) string {
	parts := make([]string, len(days))
	for i, d := range days {
		parts[i] = strconv.Itoa(d)
	}
	return fmt.Sprintf(`{"label":%q,"recurrence":%q,"start_minute":%d,"end_minute":%d,"days":[%s]}`,
		label, recurrence, startMin, endMin, strings.Join(parts, ","))
}

// at builds an instant on a fixed date in a location.
func at(loc *time.Location, hour, min int) time.Time {
	return time.Date(2026, 10, 12, hour, min, 0, 0, loc)
}

// weekdayAt returns the first instant on or after `from` that falls on `want`, so a
// fixture cannot silently rot if the calendar moves.
func weekdayAt(from time.Time, want time.Weekday) time.Time {
	for i := 0; i < 7; i++ {
		if from.Weekday() == want {
			return from
		}
		from = from.AddDate(0, 0, 1)
	}
	return from
}

func TestRecurringDailyWindow(t *testing.T) {
	// 02:00–04:00 every day.
	w := ParseMaintenanceWindows("[" + recurring("nightly", "daily", 2*60, 4*60, nil) + "]")
	utc := time.UTC

	for _, c := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"just before", at(utc, 1, 59), false},
		{"at the start", at(utc, 2, 0), true},
		{"in the middle", at(utc, 3, 0), true},
		{"at the end", at(utc, 4, 0), false},
		{"later that day", at(utc, 23, 0), false},
		// Recurring means it comes back tomorrow.
		{"the next day", at(utc, 3, 0).AddDate(0, 0, 1), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ActiveWindow(w, c.at, "S1", utc) != nil; got != c.want {
				t.Errorf("at %s = %v, want %v", c.at.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// A window that crosses midnight has to cover both sides of it, or "23:00–02:00"
// would only ever mean 23:00–00:00.
func TestRecurringWindowAcrossMidnight(t *testing.T) {
	w := ParseMaintenanceWindows("[" + recurring("overnight", "daily", 23*60, 2*60, nil) + "]")
	utc := time.UTC
	day := at(utc, 0, 0)

	for _, c := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"22:59", day.Add(22*time.Hour + 59*time.Minute), false},
		{"23:00", day.Add(23 * time.Hour), true},
		{"23:59", day.Add(23*time.Hour + 59*time.Minute), true},
		{"00:30 next day", day.Add(24*time.Hour + 30*time.Minute), true},
		{"01:59 next day", day.Add(25*time.Hour + 59*time.Minute), true},
		{"02:00 next day", day.Add(26 * time.Hour), false},
		{"midday next day", day.Add(36 * time.Hour), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ActiveWindow(w, c.at, "S1", utc) != nil; got != c.want {
				t.Errorf("at %s = %v, want %v", c.at.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

func TestRecurringWeeklyWindow(t *testing.T) {
	// Monday and Friday, 02:00–04:00.
	w := ParseMaintenanceWindows("[" + recurring("mon & fri", "weekly", 2*60, 4*60, []int{1, 5}) + "]")
	utc := time.UTC
	monday := weekdayAt(at(utc, 3, 0), time.Monday)

	for _, c := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"Monday", monday, true},
		{"Tuesday", monday.AddDate(0, 0, 1), false},
		{"Friday", monday.AddDate(0, 0, 4), true},
		{"Saturday", monday.AddDate(0, 0, 5), false},
		{"next Monday", monday.AddDate(0, 0, 7), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ActiveWindow(w, c.at, "S1", utc) != nil; got != c.want {
				t.Errorf("at %s = %v, want %v", c.at.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// A weekly window that crosses midnight must cover the morning after the listed day,
// or a "Friday night" window would stop at midnight and miss the small hours.
func TestRecurringWeeklyAcrossMidnight(t *testing.T) {
	w := ParseMaintenanceWindows("[" + recurring("Friday night", "weekly", 23*60, 2*60, []int{5}) + "]")
	utc := time.UTC
	friday := weekdayAt(at(utc, 0, 0), time.Friday)

	for _, c := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"Friday 22:00", friday.Add(22 * time.Hour), false},
		{"Friday 23:30", friday.Add(23*time.Hour + 30*time.Minute), true},
		{"Saturday 01:00", friday.Add(25 * time.Hour), true},
		{"Saturday midday", friday.Add(36 * time.Hour), false},
		{"Thursday 01:00", friday.Add(-23 * time.Hour), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ActiveWindow(w, c.at, "S1", utc) != nil; got != c.want {
				t.Errorf("at %s = %v, want %v", c.at.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// A recurring window is read on the app's clock, not UTC: "02:00" has to mean 02:00
// where the app runs, the same way cron does.
func TestRecurringWindowUsesTheDispatcherLocation(t *testing.T) {
	w := ParseMaintenanceWindows("[" + recurring("nightly", "daily", 2*60, 4*60, nil) + "]")
	// A fixed +02:00 zone, so the test does not depend on the host having tzdata.
	east := time.FixedZone("UTC+2", 2*3600)

	// 03:00 on the app's clock is 01:00 UTC: inside the window on one, outside on the
	// other, which is exactly the distinction being tested.
	instant := time.Date(2026, 10, 13, 3, 0, 0, 0, east)
	if ActiveWindow(w, instant, "S1", east) == nil {
		t.Error("the window should be open on the app's own clock")
	}
	if ActiveWindow(w, instant, "S1", time.UTC) != nil {
		t.Error("the same instant is 01:00 UTC, outside the window")
	}
}

func TestParseDropsInvalidRecurringWindows(t *testing.T) {
	for _, raw := range []string{
		"[" + recurring("zero length", "daily", 120, 120, nil) + "]",
		"[" + recurring("negative start", "daily", -1, 60, nil) + "]",
		"[" + recurring("past midnight", "daily", 100, minutesPerDay, nil) + "]",
		"[" + recurring("weekly with no days", "weekly", 120, 240, nil) + "]",
		"[" + recurring("day out of range", "weekly", 120, 240, []int{7}) + "]",
		"[" + recurring("unknown recurrence", "fortnightly", 120, 240, nil) + "]",
	} {
		if got := ParseMaintenanceWindows(raw); len(got) != 0 {
			t.Errorf("ParseMaintenanceWindows(%s) = %v, want none", raw, got)
		}
	}
}

// End to end: a daily window silences an event that arrives inside its hours.
func TestDispatchHonoursARecurringWindow(t *testing.T) {
	utc := time.UTC
	now := at(utc, 3, 0)
	st := newFakeStore(store.Channel{ID: 1, Type: "rec", Enabled: true, Events: "[]"})
	st.settings = map[string]string{
		"maintenance_windows": "[" + recurring("nightly", "daily", 2*60, 4*60, nil) + "]",
	}
	sender := &recordingSender{typ: "rec"}

	d := testDispatcher(st, sender, DispatcherOptions{
		Now: func() time.Time { return now }, Location: utc,
	})
	d.Dispatch(context.Background(), []Event{{Type: EventHealthChange, Serial: "S1"}})

	if sender.count() != 0 {
		t.Errorf("deliveries = %d, want 0 inside the nightly window", sender.count())
	}
	if entry, ok := st.onlyLogged(); !ok || entry.status != string(OutcomeSquelched) {
		t.Errorf("log entry = %+v (ok=%v), want a squelch", entry, ok)
	}
}

// --- dispatch integration -----------------------------------------------------

// The point of the feature: an event inside a window is recorded as squelched and
// never reaches the channel.
func TestDispatchSilencesInsideAMaintenanceWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	st := newFakeStore(store.Channel{ID: 1, Type: "rec", Enabled: true, Events: "[]"})
	st.settings = map[string]string{
		"maintenance_windows": "[" + window("array rebuild", now.Add(-time.Hour), now.Add(time.Hour)) + "]",
	}
	sender := &recordingSender{typ: "rec"}

	d := testDispatcher(st, sender, DispatcherOptions{Now: func() time.Time { return now }})
	d.Dispatch(context.Background(), []Event{{Type: EventHealthChange, Serial: "S1", Priority: PriorityHigh}})

	if sender.count() != 0 {
		t.Errorf("deliveries = %d, want 0 while a window is open", sender.count())
	}
	entry, ok := st.onlyLogged()
	if !ok {
		t.Fatal("the suppression should still be recorded")
	}
	if entry.status != string(OutcomeSquelched) {
		t.Errorf("status = %q, want squelched", entry.status)
	}
	// The log has to name the window, or an operator cannot tell a deliberate silence
	// from a squelch timer or a delivery failure.
	if want := "maintenance window: array rebuild"; entry.detail != want {
		t.Errorf("detail = %q, want %q", entry.detail, want)
	}
}

func TestDispatchDeliversOutsideAMaintenanceWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	st := newFakeStore(store.Channel{ID: 1, Type: "rec", Enabled: true, Events: "[]"})
	st.settings = map[string]string{
		// Yesterday's window, long closed.
		"maintenance_windows": "[" + window("yesterday", now.Add(-25*time.Hour), now.Add(-23*time.Hour)) + "]",
	}
	sender := &recordingSender{typ: "rec"}

	d := testDispatcher(st, sender, DispatcherOptions{Now: func() time.Time { return now }})
	d.Dispatch(context.Background(), []Event{{Type: EventHealthChange, Serial: "S1"}})

	if sender.count() != 1 {
		t.Errorf("deliveries = %d, want 1 once the window has closed", sender.count())
	}
}

func TestDispatchMaintenanceWindowRespectsSerialScope(t *testing.T) {
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	open := "[" + window("sda only", now.Add(-time.Hour), now.Add(time.Hour), "S1") + "]"

	for _, c := range []struct {
		serial string
		want   int
	}{
		{"S1", 0}, // covered by the window
		{"S2", 1}, // not covered
	} {
		st := newFakeStore(store.Channel{ID: 1, Type: "rec", Enabled: true, Events: "[]"})
		st.settings = map[string]string{"maintenance_windows": open}
		sender := &recordingSender{typ: "rec"}

		d := testDispatcher(st, sender, DispatcherOptions{Now: func() time.Time { return now }})
		d.Dispatch(context.Background(), []Event{{Type: EventHealthChange, Serial: c.serial}})

		if sender.count() != c.want {
			t.Errorf("serial %s: deliveries = %d, want %d", c.serial, sender.count(), c.want)
		}
	}
}

// A window must not be able to suppress the test button: the operator pressing Test
// is asking "does this channel work right now?".
func TestSendTestIgnoresMaintenanceWindows(t *testing.T) {
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	st := newFakeStore()
	st.settings = map[string]string{
		"maintenance_windows": "[" + window("everything", now.Add(-time.Hour), now.Add(time.Hour)) + "]",
	}
	sender := &recordingSender{typ: "rec"}
	d := testDispatcher(st, sender, DispatcherOptions{Now: func() time.Time { return now }})

	outcome, err := d.SendTest(context.Background(),
		store.Channel{ID: 1, Type: "rec", Enabled: true}, Event{Type: EventHealthChange, Serial: "S1"})
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if outcome != OutcomeSent {
		t.Errorf("outcome = %q, want sent", outcome)
	}
}
