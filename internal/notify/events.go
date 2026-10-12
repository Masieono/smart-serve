package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/store"
)

// DefaultTemperatureThresholdC is the alert threshold when the setting is unset
// (docs/NOTIFICATIONS.md).
const DefaultTemperatureThresholdC = 55

// CollectOptions tunes event generation.
type CollectOptions struct {
	TemperatureThresholdC int
	// TemperatureUnit is the display unit for human-readable output ("c" or "f").
	// The wire payload always carries Celsius, because that is what drives report.
	TemperatureUnit string
	Version         string
}

// Collect turns a finished scan into the events worth notifying about.
//
// Transition detection is the dedupe mechanism (ADR-008): a persistently bad
// verdict produces no further health_change events, so a failing drive cannot
// spam every scheduled scan.
func Collect(rep scan.PostScan, opts CollectOptions) []Event {
	ts := rep.Ended.UnixMilli()
	base := func(typ string, pri Priority, st store.DeviceState) Event {
		return Event{
			Type: typ, Priority: pri,
			Serial: st.Serial, DeviceName: st.Name, DeviceModel: st.Model,
			TemperatureC: st.TemperatureC, ScanTS: ts, Version: opts.Version,
			tempUnit: opts.TemperatureUnit,
		}
	}

	// A failed *scheduled* scan is news; a failed manual one is already visible
	// to whoever pressed the button.
	if !rep.OK {
		if rep.Trigger.Kind != "schedule" {
			return nil
		}
		return []Event{{
			Type: EventScanFailure, Priority: PriorityMedium,
			ScheduleID: rep.Trigger.ScheduleID, Error: rep.Error,
			ScanTS: ts, Version: opts.Version,
			key: fmt.Sprintf("%s:%d", EventScanFailure, rep.Trigger.ScheduleID),
		}}
	}

	// The very first scan would otherwise announce every drive as "detected".
	firstScan := len(rep.Previous) == 0

	events := make([]Event, 0, 4)
	for serial, cur := range rep.Current {
		prev, known := rep.Previous[serial]

		switch {
		case cur.Missing:
			// Gone. Only a fresh disappearance is worth an alert.
			if known && !prev.Missing {
				ev := base(EventDeviceDisappeared, PriorityMedium, prev)
				ev.key = EventDeviceDisappeared + ":" + serial
				events = append(events, ev)
			}
			continue
		case !known || prev.Missing:
			// Newly seen, or back after being unplugged.
			if !firstScan {
				ev := base(EventDeviceAppeared, PriorityLow, cur)
				ev.key = EventDeviceAppeared + ":" + serial
				events = append(events, ev)
			}
		case prev.Health != cur.Health:
			ev := base(EventHealthChange, PriorityHigh, cur)
			ev.OldHealth, ev.NewHealth = prev.Health, cur.Health
			ev.Rationale = cur.HealthRationale
			ev.key = EventHealthChange + ":" + serial + ":" + cur.Health
			events = append(events, ev)
		}

		if opts.TemperatureThresholdC > 0 && cur.TemperatureC != nil &&
			*cur.TemperatureC >= opts.TemperatureThresholdC {
			ev := base(EventTemperatureHigh, PriorityMedium, cur)
			ev.Rationale = fmt.Sprintf("%s is at or above the %s threshold",
				formatTemp(*cur.TemperatureC, opts.TemperatureUnit),
				formatTemp(opts.TemperatureThresholdC, opts.TemperatureUnit))
			ev.key = EventTemperatureHigh + ":" + serial
			events = append(events, ev)
		}
	}

	// Map iteration order is random; sort by serial then type so the output (and
	// the tests) are stable.
	sort.Slice(events, func(i, j int) bool {
		if events[i].Serial != events[j].Serial {
			return events[i].Serial < events[j].Serial
		}
		return events[i].Type < events[j].Type
	})
	return events
}

// Settings reads the tunables that live in the database (ADR-009).
type Settings interface {
	GetSettings(ctx context.Context) (map[string]string, error)
}

// Notifier bridges finished scans to the dispatcher. It is the post-scan hook
// registered on the scan manager.
type Notifier struct {
	settings Settings
	disp     *Dispatcher
	log      *slog.Logger
	version  string
}

// NewNotifier wires a dispatcher to the settings store.
func NewNotifier(st Settings, disp *Dispatcher, version string, log *slog.Logger) *Notifier {
	if log == nil {
		log = slog.Default()
	}
	return &Notifier{settings: st, disp: disp, log: log, version: version}
}

// OnScan implements scan.PostScanFunc. It must never fail the scan, so every
// problem here is logged rather than returned.
func (n *Notifier) OnScan(ctx context.Context, rep scan.PostScan) {
	threshold, unit := n.temperatureSettings(ctx)
	events := Collect(rep, CollectOptions{
		TemperatureThresholdC: threshold,
		TemperatureUnit:       unit,
		Version:               n.version,
	})
	if len(events) == 0 {
		return
	}
	n.log.Info("dispatching notifications", "events", len(events), "trigger", rep.Trigger.Kind)
	n.disp.Dispatch(ctx, events)
}

// temperatureSettings reads the user-tunable temperature settings, falling back to
// the documented defaults whenever they are missing or unparseable. The unit only
// affects how alerts are worded; the threshold itself is always Celsius.
func (n *Notifier) temperatureSettings(ctx context.Context) (threshold int, unit string) {
	threshold, unit = DefaultTemperatureThresholdC, "c"
	settings, err := n.settings.GetSettings(ctx)
	if err != nil {
		return threshold, unit
	}
	if parsed, perr := strconv.Atoi(strings.TrimSpace(settings["temperature_threshold_c"])); perr == nil && parsed > 0 {
		threshold = parsed
	}
	if strings.EqualFold(strings.TrimSpace(settings["temperature_unit"]), "f") {
		unit = "f"
	}
	return threshold, unit
}

// TestEvent is the synthetic event behind "send test", so an operator can see
// exactly what a real alert looks like before trusting it.
func TestEvent(version string) Event {
	return Event{
		Type:        EventHealthChange,
		Priority:    PriorityHigh,
		Serial:      "SMARTSERVE-TEST",
		DeviceName:  "smart-serve test",
		DeviceModel: "Synthetic device",
		OldHealth:   "good",
		NewHealth:   "caution",
		Rationale:   "This is a test notification from Smart-Serve.",
		ScanTS:      time.Now().UnixMilli(),
		Version:     version,
		key:         "test",
	}
}
