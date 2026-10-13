package notify

import (
	"encoding/json"
	"strings"
	"time"
)

// MaintenanceWindow silences alerts for a period — a disk-array rebuild, a
// migration, a chassis being moved — without disabling a channel and having to
// remember to re-enable it (docs/NOTIFICATIONS.md, FEATURES.md B8).
//
// Windows are stored as a setting and evaluated at dispatch time. Nothing sleeps
// until a window opens or closes, so they add no idle cost (FEATURES.md §E).
type MaintenanceWindow struct {
	Label    string `json:"label,omitempty"`
	StartsAt int64  `json:"starts_at"` // unix milliseconds
	EndsAt   int64  `json:"ends_at"`   // unix milliseconds
	// Serials limits the window to specific drives; empty covers every drive.
	Serials []string `json:"serials,omitempty"`
}

// Name renders the window for the delivery log.
func (w MaintenanceWindow) Name() string {
	if label := strings.TrimSpace(w.Label); label != "" {
		return label
	}
	return "maintenance window"
}

// covers reports whether the window applies to a drive. An empty list covers all.
func (w MaintenanceWindow) covers(serial string) bool {
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

// ParseMaintenanceWindows decodes the setting.
//
// Anything malformed is dropped rather than failing the pass: a bad window must
// never be able to silence alerts, and it must never be able to stop delivery
// either. A window whose end is not after its start would do one or the other, so
// it is discarded.
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
		if w.EndsAt <= w.StartsAt {
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
func ActiveWindow(windows []MaintenanceWindow, now time.Time, serial string) *MaintenanceWindow {
	if len(windows) == 0 {
		return nil
	}
	ms := now.UnixMilli()
	var found *MaintenanceWindow
	for i := range windows {
		w := &windows[i]
		if ms < w.StartsAt || ms >= w.EndsAt {
			continue
		}
		if !w.covers(serial) {
			continue
		}
		found = w
	}
	return found
}
