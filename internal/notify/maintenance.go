package notify

import (
	"encoding/json"
	"strings"
	"time"
)

// minutesPerDay bounds a time-of-day window.
const minutesPerDay = 24 * 60

// MaintenanceWindow silences alerts for a period — a disk-array rebuild, a
// migration, a chassis being moved — without disabling a channel and having to
// remember to re-enable it (docs/NOTIFICATIONS.md, FEATURES.md B8).
//
// A window is either **one-off** (absolute `starts_at`/`ends_at`) or **recurring**
// (`daily`/`weekly` with a time of day). Windows are stored as a setting and
// evaluated at dispatch time; nothing sleeps until a window opens or closes, so
// they add no idle cost (FEATURES.md §E).
type MaintenanceWindow struct {
	Label string `json:"label,omitempty"`
	// Serials limits the window to specific drives; empty covers every drive.
	Serials []string `json:"serials,omitempty"`

	// Recurrence is "once", "daily" or "weekly"; empty means "once".
	Recurrence string `json:"recurrence,omitempty"`

	// One-off: absolute instants in unix milliseconds.
	StartsAt int64 `json:"starts_at,omitempty"`
	EndsAt   int64 `json:"ends_at,omitempty"`

	// Recurring: minutes past midnight in the dispatcher's location — the same clock
	// the scheduler evaluates cron against, so "02:00" means 02:00 where the app runs.
	StartMinute int `json:"start_minute,omitempty"`
	EndMinute   int `json:"end_minute,omitempty"`
	// Days are weekday numbers (0 = Sunday), used by "weekly".
	Days []int `json:"days,omitempty"`
}

// Repeats reports whether the window is recurring rather than a single interval.
func (w MaintenanceWindow) Repeats() bool {
	return w.Recurrence == "daily" || w.Recurrence == "weekly"
}

// Name renders the window for the delivery log.
func (w MaintenanceWindow) Name() string {
	if label := strings.TrimSpace(w.Label); label != "" {
		return label
	}
	return "maintenance window"
}

// appliesTo reports whether the window covers a drive. An empty list covers all.
func (w MaintenanceWindow) appliesTo(serial string) bool {
	if len(w.Serials) == 0 {
		return true
	}
	for _, s := range w.Serials {
		if serial != "" && strings.EqualFold(strings.TrimSpace(s), serial) {
			return true
		}
	}
	return false
}

// valid reports whether the window is usable. Anything else is dropped at parse
// time rather than being allowed to silence alerts in a way nobody can explain.
func (w MaintenanceWindow) valid() bool {
	inDay := func(m int) bool { return m >= 0 && m < minutesPerDay }
	switch w.Recurrence {
	case "", "once":
		return w.EndsAt > w.StartsAt
	case "daily":
		// A zero-length window would never apply, so equal ends are a mistake.
		return inDay(w.StartMinute) && inDay(w.EndMinute) && w.StartMinute != w.EndMinute
	case "weekly":
		if !inDay(w.StartMinute) || !inDay(w.EndMinute) || w.StartMinute == w.EndMinute {
			return false
		}
		if len(w.Days) == 0 {
			return false
		}
		for _, d := range w.Days {
			if d < 0 || d > 6 {
				return false
			}
		}
		return true
	}
	return false
}

// covers reports whether the window silences the given instant.
func (w MaintenanceWindow) covers(at time.Time, loc *time.Location) bool {
	if !w.Repeats() {
		ms := at.UnixMilli()
		return ms >= w.StartsAt && ms < w.EndsAt
	}

	local := at.In(loc)
	minutes := local.Hour()*60 + local.Minute()
	day := local.Weekday()

	if w.StartMinute < w.EndMinute {
		if minutes < w.StartMinute || minutes >= w.EndMinute {
			return false
		}
	} else {
		// Crosses midnight. The evening part belongs to today; the morning part
		// belongs to the day the window actually started on, which is yesterday —
		// otherwise a weekly Friday 23:00–02:00 window would never cover Saturday.
		switch {
		case minutes >= w.StartMinute:
			// still on the starting day
		case minutes < w.EndMinute:
			day = local.AddDate(0, 0, -1).Weekday()
		default:
			return false
		}
	}

	if w.Recurrence == "daily" {
		return true
	}
	for _, d := range w.Days {
		if int(day) == d {
			return true
		}
	}
	return false
}

// ParseMaintenanceWindows decodes the setting.
//
// Anything malformed is dropped rather than failing the pass: a bad window must
// never be able to silence alerts, and it must never be able to stop delivery
// either.
func ParseMaintenanceWindows(raw string) []MaintenanceWindow {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var decoded []MaintenanceWindow
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}

	out := make([]MaintenanceWindow, 0, len(decoded))
	for _, w := range decoded {
		if !w.valid() {
			continue
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ActiveWindow returns the window silencing this event, or nil. Later windows win,
// so a narrower window added after a broad one can carve out an exception.
func ActiveWindow(windows []MaintenanceWindow, now time.Time, serial string, loc *time.Location) *MaintenanceWindow {
	if len(windows) == 0 {
		return nil
	}
	if loc == nil {
		loc = time.Local
	}
	var found *MaintenanceWindow
	for i := range windows {
		w := &windows[i]
		if !w.appliesTo(serial) || !w.covers(now, loc) {
			continue
		}
		found = w
	}
	return found
}
