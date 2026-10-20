package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
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
	// Correlation tunes the group-heat check (FEATURES.md A25). A zero MinDrives turns it off, which
	// is what a caller with no settings has.
	Correlation interp.CorrelationThresholds
	// BaseURL is where this deployment is reachable, so a channel that supports it can offer a link
	// back into the app (FEATURES.md B10). Empty when the operator has not set one.
	BaseURL string
	Version string
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
			tempUnit: opts.TemperatureUnit, baseURL: opts.BaseURL,
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
			baseURL: opts.BaseURL,
			key:     fmt.Sprintf("%s:%d", EventScanFailure, rep.Trigger.ScheduleID),
		}}
	}

	// The very first scan would otherwise announce every drive as "detected".
	firstScan := len(rep.Previous) == 0

	events := make([]Event, 0, 4)
	for serial, cur := range rep.Current {
		prev, known := rep.Previous[serial]

		// An acknowledged condition is one the user has said they know about (`FEATURES.md` B7). It
		// silences the two alerts that describe the *condition* — a verdict change and a hot drive —
		// and nothing else: a drive that vanishes is still news, and a self-test result is something
		// the user asked for.
		//
		// The ack is spent the moment the verdict moves, because `AckHealth == cur.Health` is only
		// true while the drive is in the state the user named. So it can never hide a drive getting
		// worse, which is the one thing it must not do.
		acked := cur.AckHealth != "" && cur.AckHealth == cur.Health

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
		case prev.Health != cur.Health && !acked:
			ev := base(EventHealthChange, PriorityHigh, cur)
			ev.OldHealth, ev.NewHealth = prev.Health, cur.Health
			ev.Rationale = cur.HealthRationale
			ev.key = EventHealthChange + ":" + serial + ":" + cur.Health
			events = append(events, ev)
		}

		if !acked && opts.TemperatureThresholdC > 0 && cur.TemperatureC != nil &&
			*cur.TemperatureC >= opts.TemperatureThresholdC {
			ev := base(EventTemperatureHigh, PriorityMedium, cur)
			ev.Rationale = fmt.Sprintf("%s is at or above the %s threshold",
				formatTemp(*cur.TemperatureC, opts.TemperatureUnit),
				formatTemp(opts.TemperatureThresholdC, opts.TemperatureUnit))
			ev.key = EventTemperatureHigh + ":" + serial
			events = append(events, ev)
		}

		// B9: a self-test result that was not there last time we looked.
		//
		// Identity is type and hours, because a self-test log entry carries no id of its own.
		// Two identical tests in the same power-on hour are therefore indistinguishable and the
		// second will not notify — a rare miss, and the alternative is announcing a result that
		// has already been announced.
		//
		// Gated on SelfTestRecorded so that upgrading does not announce every drive's existing
		// results: a row the app has never read has no baseline to differ from, and "no baseline"
		// is a different finding from "an empty log".
		if cur.SelfTest != nil && prev.SelfTestRecorded && !sameSelfTest(prev.SelfTest, cur.SelfTest) {
			ev := base(EventSelfTestResult, PriorityLow, cur)
			if cur.SelfTest.Passed != nil && !*cur.SelfTest.Passed {
				// A failure is the entire reason to run a self-test, so it is not a low-priority
				// note.
				ev.Priority = PriorityHigh
			}
			ev.SelfTestType = cur.SelfTest.Type
			ev.SelfTestPassed = cur.SelfTest.Passed
			ev.SelfTestHours = cur.SelfTest.Hours
			ev.Rationale = selfTestRationale(cur.SelfTest)
			ev.key = EventSelfTestResult + ":" + serial
			events = append(events, ev)
		}
	}

	// A25: the groups whose drives moved together. Built from this scan and the one before it, which
	// is a sharper baseline than any time window — the question is "what changed since we last
	// looked", and a scan boundary is exactly that.
	//
	// Before the heartbeat, deliberately: a scan that found a chassis heating up has something to
	// say, and the heartbeat exists for the scans that do not.
	events = append(events, groupHeatEvents(rep, opts)...)

	// The heartbeat fires only when the scan found nothing to say — no verdict changed, no
	// drive vanished or appeared, nothing ran hot. It is the *complement* of the alert
	// stream rather than an addition to it: you hear from a scheduled scan either way, and
	// "3 drives scanned in 4.2s" means all quiet.
	//
	// Gating on an empty event list rather than on a health check is what makes that exact.
	// The alternative — "every drive is good" — would never fire for a drive with no
	// S.M.A.R.T. (verdict unknown) or one that has been failing for a week (no transition,
	// so no alert), and both of those are precisely when a reassuring heartbeat is wanted.
	//
	// A manual scan never gets one: whoever pressed the button is watching the screen.
	if rep.Trigger.Kind == "schedule" && len(events) == 0 {
		events = append(events, Event{
			Type: EventScanSuccess, Priority: PriorityLow,
			ScheduleID:   rep.Trigger.ScheduleID,
			DevicesTotal: len(rep.Current),
			DurationMS:   rep.Ended.Sub(rep.Started).Milliseconds(),
			ScanTS:       ts, Version: opts.Version,
			baseURL: opts.BaseURL,
			key:     fmt.Sprintf("%s:%d", EventScanSuccess, rep.Trigger.ScheduleID),
		})
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

// sameSelfTest reports whether two stored results are the same test.
//
// Type and hours are the identity, because a self-test log entry carries no id of its own. The
// verdict is deliberately not part of it: a result re-read with the same type and hours but a
// different verdict is the same test read twice, not a new one.
func sameSelfTest(a, b *model.SelfTestEntry) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Type != b.Type {
		return false
	}
	if a.Hours == nil || b.Hours == nil {
		return a.Hours == nil && b.Hours == nil
	}
	return *a.Hours == *b.Hours
}

// selfTestRationale words a result for a notification.
//
// The drive's own status text is quoted where it gave one, because a notification's body is the
// only part most people read and "Completed: read failure" says more than "failed" ever could.
func selfTestRationale(e *model.SelfTestEntry) string {
	outcome := "finished"
	if e.Passed != nil {
		if *e.Passed {
			outcome = "passed"
		} else {
			outcome = "failed"
		}
	}
	what := e.Type
	if what == "" {
		what = "Self-test"
	}
	if e.Status != "" {
		return fmt.Sprintf("%s %s: %s", what, outcome, e.Status)
	}
	return fmt.Sprintf("%s %s", what, outcome)
}

// groupHeatEvents reports the tag groups whose drives heated up together (`FEATURES.md` A25).
//
// One event per group rather than one per drive, which is the whole point: eight drives in a chassis
// have one fan, and eight alerts about one fan is precisely the noise that teaches people to ignore
// alerts.
//
// It says what moved and where to look, and never what is wrong. A RAID rebuild heats everything,
// and so does a warm afternoon; the reader knows which is running and this does not.
//
// Bench drives are left out — a drive being tested is *expected* to heat up, and a batch of them on
// one bench would fire this on every scan — and so is a drive with no temperature at either reading,
// because it cannot have moved.
func groupHeatEvents(rep scan.PostScan, opts CollectOptions) []Event {
	if opts.Correlation.MinDrives < 2 {
		return nil
	}

	readings := make([]interp.HeatReading, 0, len(rep.Current))
	for serial, cur := range rep.Current {
		if cur.Missing || cur.TemperatureC == nil || store.HasTag(cur.Tags, store.BenchTag) {
			continue
		}
		r := interp.HeatReading{
			Serial: serial,
			Groups: store.TagList(cur.Tags),
			Now:    cur.TemperatureC,
		}
		if prev, ok := rep.Previous[serial]; ok && !prev.Missing {
			r.Prev = prev.TemperatureC
		}
		readings = append(readings, r)
	}

	var out []Event
	for _, g := range interp.CorrelatedHeat(readings, opts.Correlation) {
		delta := g.DeltaC
		out = append(out, Event{
			Type:        EventGroupHeat,
			Priority:    PriorityMedium,
			Group:       g.Group,
			GroupSize:   g.Total,
			GroupDrives: g.Drives,
			GroupDeltaC: &delta,
			Rationale:   interp.HeatSummary(g, formatTempDelta(delta, opts.TemperatureUnit)),
			ScanTS:      rep.Ended.UnixMilli(),
			Version:     opts.Version,
			tempUnit:    opts.TemperatureUnit,
			baseURL:     opts.BaseURL,
			// One key per group, so squelching quiets the *group* rather than the event type: a
			// second enclosure heating up while the first is squelched is news.
			key: EventGroupHeat + ":" + g.Group,
		})
	}
	return out
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
	events := Collect(rep, n.alertSettings(ctx))
	if len(events) == 0 {
		return
	}
	n.log.Info("dispatching notifications", "events", len(events), "trigger", rep.Trigger.Kind)
	n.disp.Dispatch(ctx, events)
}

// alertSettings reads the user-tunable alert settings, falling back to the documented defaults
// whenever they are missing or unparseable. The unit only affects how alerts are worded; the
// thresholds themselves are always Celsius.
//
// It returns the whole option set rather than the individual values, because there are now four of
// them and a four-value return is one positional mistake away from a temperature threshold being
// used as a URL.
func (n *Notifier) alertSettings(ctx context.Context) CollectOptions {
	opts := CollectOptions{
		TemperatureThresholdC: DefaultTemperatureThresholdC,
		TemperatureUnit:       "c",
		Correlation:           interp.DefaultCorrelationThresholds(),
		Version:               n.version,
	}
	settings, err := n.settings.GetSettings(ctx)
	if err != nil {
		return opts
	}
	if parsed, perr := strconv.Atoi(strings.TrimSpace(settings["temperature_threshold_c"])); perr == nil && parsed > 0 {
		opts.TemperatureThresholdC = parsed
	}
	if strings.EqualFold(strings.TrimSpace(settings["temperature_unit"]), "f") {
		opts.TemperatureUnit = "f"
	}
	opts.Correlation = interp.CorrelationFromSettings(settings)
	// Where the app is reachable from wherever the alert lands (`FEATURES.md` B10). Empty is the
	// default and means "no action button", which is honest: the app cannot know its own external
	// address, and guessing `localhost` would produce a link that works for nobody.
	opts.BaseURL = strings.TrimSpace(settings["public_url"])
	return opts
}

// TestEvent is the synthetic event behind "send test". Its Title and Body say plainly
// that it is a test — an operator pressing Test is asking "does this channel work?",
// not "what does an alert look like" — while the payload keeps a realistic shape so a
// consumer's parser is exercised by it.
func TestEvent(version string) Event {
	return Event{
		Type:     EventHealthChange,
		Priority: PriorityHigh,
		Test:     true,
		// Unmistakably synthetic: these only reach the JSON payload, since Title and
		// Body are replaced wholesale for a test.
		Serial:      "SMARTSERVE-TEST",
		DeviceName:  "Smart-Serve test",
		DeviceModel: "synthetic",
		OldHealth:   "good",
		NewHealth:   "caution",
		Rationale:   "This is a test notification from Smart-Serve.",
		ScanTS:      time.Now().UnixMilli(),
		Version:     version,
		key:         "test",
	}
}
