// Package notify delivers scan events to configured channels
// (docs/NOTIFICATIONS.md).
//
// Delivery is deliberately best-effort. A broken channel logs a failure and can
// never fail the scan that produced the event, and the whole post-scan pass is
// bounded by a budget so a hung endpoint cannot delay the next scheduled scan.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/store"
)

// Event types (docs/NOTIFICATIONS.md event catalog).
const (
	EventHealthChange      = "health_change"
	EventTemperatureHigh   = "temperature_high"
	EventDeviceDisappeared = "device_disappeared"
	EventDeviceAppeared    = "device_appeared"
	EventScanFailure       = "scan_failure"
	// EventSelfTestResult reports the outcome of a self-test (FEATURES.md B9). One event type
	// rather than two, because it is always the result of a test someone deliberately started:
	// unlike the heartbeat it can never arrive unsolicited, so it belongs in "everything" and
	// the outcome rides in the payload instead of the type.
	EventSelfTestResult = "self_test_result"
	// EventScanSuccess is a heartbeat, not an alert: it says the scheduled scan ran. It is
	// opt-in (see optInEvents) because it fires on every scheduled scan rather than on a
	// change, and "everything" was never meant to include a periodic all-clear.
	EventScanSuccess = "scan_success"
	// EventGroupHeat reports that several drives in one tag group heated up together
	// (FEATURES.md A25), which points at what they share — a fan, an intake, an ambient —
	// rather than at the drives.
	//
	// It is **opt-in**, and that is the mitigation for the one thing that can go wrong here: a RAID
	// rebuild, or a warm afternoon, also heats everything, and nothing observable from a temperature
	// tells them apart (`ADR-023`). The body therefore states a correlation rather than a diagnosis,
	// and a channel has to ask for it — so an install that would find it noisy never receives it.
	EventGroupHeat = "group_heat"
)

// EventTypes lists every subscribable event, in catalog order.
var EventTypes = []string{
	EventHealthChange,
	EventTemperatureHigh,
	EventDeviceDisappeared,
	EventDeviceAppeared,
	EventScanFailure,
	EventSelfTestResult,
	EventScanSuccess,
	EventGroupHeat,
}

// optInEvents are the events a channel has to ask for by name.
//
// An empty subscription list means "everything", which is right for exceptions — a
// transition, a failure, a drive vanishing — and wrong for a heartbeat. Adding one to the
// catalog must not start sending it to every channel that never asked, least of all on
// upgrade: on a 15-minute schedule that would turn a quiet install into 96 messages a day,
// and the first thing a user would do is turn notifications off altogether.
//
// An explicit "*" still means everything, including these.
var optInEvents = map[string]bool{EventScanSuccess: true, EventGroupHeat: true}

// Priority influences formatting (the ntfy priority header, a payload field) and
// squelch defaults — never whether a subscribed channel receives the event.
type Priority string

// Priorities, lowest first.
const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

// Event is one notification-worthy occurrence.
type Event struct {
	Type         string   `json:"event"`
	Priority     Priority `json:"priority"`
	Serial       string   `json:"device_serial,omitempty"`
	DeviceName   string   `json:"device_name,omitempty"`
	DeviceModel  string   `json:"device_model,omitempty"`
	OldHealth    string   `json:"old_health,omitempty"`
	NewHealth    string   `json:"new_health,omitempty"`
	Rationale    string   `json:"rationale,omitempty"`
	TemperatureC *int     `json:"temperature_c,omitempty"`
	ScheduleID   int64    `json:"schedule_id,omitempty"`
	Error        string   `json:"error,omitempty"`
	// Scan-summary fields, used by the scheduled-scan heartbeat.
	DevicesTotal int   `json:"devices_total,omitempty"`
	DurationMS   int64 `json:"duration_ms,omitempty"`
	// Self-test result fields, used by `self_test_result` (FEATURES.md B9). The verdict is a
	// pointer so a drive that gave none is distinguishable from one that failed.
	SelfTestType   string `json:"self_test_type,omitempty"`
	SelfTestPassed *bool  `json:"self_test_passed,omitempty"`
	SelfTestHours  *int64 `json:"self_test_hours,omitempty"`
	// Group-heat fields, used by `group_heat` (FEATURES.md A25). The group replaces the device
	// identity, because the finding is about the group rather than about any one drive in it.
	Group       string   `json:"group,omitempty"`
	GroupSize   int      `json:"group_size,omitempty"`
	GroupDrives []string `json:"group_drives,omitempty"`
	GroupDeltaC *int     `json:"group_delta_c,omitempty"`
	ScanTS      int64    `json:"scan_ts"`
	Version     string   `json:"smartserve_version,omitempty"`
	// Test marks a synthetic event from the "send test" button. It is on the wire on
	// purpose: an automation watching a webhook has to be able to tell a test from a
	// real alert and decline to act on it.
	Test bool `json:"test,omitempty"`

	// key and tempUnit are presentation details and are deliberately not on the
	// wire: the payload always carries canonical Celsius, which is what a machine
	// consumer wants. They only shape the human-readable output.
	key      string
	tempUnit string
}

// Key is the squelch key for the event (docs/NOTIFICATIONS.md).
func (e Event) Key() string {
	if e.key != "" {
		return e.key
	}
	return e.Type
}

// Title is the one-line summary. It is used as the ntfy Title header, so it must
// stay printable ASCII.
func (e Event) Title() string {
	if e.Test {
		return "Test from Smart-Serve"
	}
	switch e.Type {
	case EventHealthChange:
		return "Drive health changed: " + e.label()
	case EventTemperatureHigh:
		return "Drive running hot: " + e.label()
	case EventDeviceDisappeared:
		return "Drive disconnected: " + e.label()
	case EventDeviceAppeared:
		return "Drive detected: " + e.label()
	case EventScanFailure:
		return "Scheduled scan failed"
	case EventScanSuccess:
		return "Scheduled scan completed"
	case EventGroupHeat:
		return "Drives heating together: " + e.Group
	}
	return "Smart-Serve: " + e.Type
}

// Body is the message text. The first line repeats the title so the message still
// reads correctly in clients that do not render the Title header.
func (e Event) Body() string {
	// A test is not shaped like a drive alert. "Drive health changed" for a device that
	// does not exist is confusing in a notification tray, and in the JSON it is worse:
	// it can trip an automation that is watching for real alerts.
	if e.Test {
		lines := []string{
			e.emoji() + " Test from Smart-Serve",
			"This is a test notification. If you can read it, this channel is working.",
		}
		if e.Version != "" {
			lines = append(lines, "Smart-Serve "+e.Version)
		}
		return strings.Join(lines, "\n")
	}

	lines := []string{e.emoji() + " " + e.Title()}

	switch e.Type {
	case EventHealthChange:
		lines = append(lines, fmt.Sprintf("%s → %s", e.OldHealth, e.NewHealth))
		if e.Rationale != "" {
			lines = append(lines, e.Rationale)
		}
	case EventTemperatureHigh:
		if e.TemperatureC != nil {
			lines = append(lines, formatTemp(*e.TemperatureC, e.tempUnit))
		}
		if e.Rationale != "" {
			lines = append(lines, e.Rationale)
		}
	case EventGroupHeat:
		if e.GroupDeltaC != nil {
			lines = append(lines, formatTempDelta(*e.GroupDeltaC, e.tempUnit)+
				" hotter than at the previous reading")
		}
		if e.Rationale != "" {
			lines = append(lines, e.Rationale)
		}
	case EventScanFailure:
		if e.Error != "" {
			lines = append(lines, e.Error)
		}
	case EventScanSuccess:
		lines = append(lines, e.scanSummary())
	}

	detail := make([]string, 0, 3)
	if e.Serial != "" {
		detail = append(detail, "Serial "+e.Serial)
	}
	if e.TemperatureC != nil && e.Type != EventTemperatureHigh {
		detail = append(detail, "temp "+formatTemp(*e.TemperatureC, e.tempUnit))
	}
	if e.ScanTS > 0 {
		detail = append(detail, "last scan "+time.UnixMilli(e.ScanTS).Format("15:04"))
	}
	if len(detail) > 0 {
		lines = append(lines, strings.Join(detail, ", "))
	}
	return strings.Join(lines, "\n")
}

// formatTemp renders a Celsius reading in the user's chosen display unit. The wire
// payload always carries Celsius; this is for human-readable output only, so an
// alert matches whatever unit the UI is showing.
func formatTemp(celsius int, unit string) string {
	if strings.EqualFold(strings.TrimSpace(unit), "f") {
		return fmt.Sprintf("%d °F", int(math.Round(float64(celsius)*9/5+32)))
	}
	return fmt.Sprintf("%d °C", celsius)
}

// formatTempDelta renders a temperature *span*, which scales with the unit but takes no offset.
//
// The distinction is not pedantry: an 8 °C rise is a 14 °F rise, and running it through `formatTemp`
// would report 46 °F — a number that reads as a temperature rather than as a change, in the one
// message whose whole job is to describe how much things moved.
func formatTempDelta(celsius int, unit string) string {
	if strings.EqualFold(strings.TrimSpace(unit), "f") {
		return fmt.Sprintf("%d °F", int(math.Round(float64(celsius)*9/5)))
	}
	return fmt.Sprintf("%d °C", celsius)
}

// label names the device as concisely as the available fields allow.
func (e Event) label() string {
	switch {
	case e.DeviceName != "" && e.DeviceModel != "":
		return fmt.Sprintf("%s (%s)", e.DeviceName, e.DeviceModel)
	case e.DeviceName != "":
		return e.DeviceName
	case e.DeviceModel != "":
		return e.DeviceModel
	case e.Serial != "":
		return e.Serial
	}
	return "device"
}

func (e Event) emoji() string {
	if e.Test {
		return "🔔"
	}
	switch e.Type {
	case EventHealthChange:
		switch e.NewHealth {
		case "good":
			return "✅"
		case "bad":
			return "🛑"
		}
		return "⚠️"
	case EventTemperatureHigh:
		return "🌡️"
	case EventDeviceDisappeared, EventDeviceAppeared:
		return "🔌"
	case EventScanFailure:
		return "🚨"
	case EventScanSuccess:
		return "✅"
	case EventGroupHeat:
		return "🌡️"
	}
	return "💾"
}

// scanSummary is the heartbeat's one line: how much was looked at, and how long it took.
//
// It deliberately does not report per-device problems. Those have their own alerts, and a
// heartbeat that also editorialises would be a second, worse copy of them.
func (e Event) scanSummary() string {
	if e.DevicesTotal <= 0 {
		return "Scan completed"
	}
	noun := "drives"
	if e.DevicesTotal == 1 {
		noun = "drive"
	}
	return fmt.Sprintf("%d %s scanned in %.1fs", e.DevicesTotal, noun, float64(e.DurationMS)/1000)
}

// Outcome is the result of one delivery attempt, recorded in notification_log.
type Outcome string

// Delivery outcomes.
const (
	OutcomeSent      Outcome = "sent"
	OutcomeFailed    Outcome = "failed"
	OutcomeSquelched Outcome = "squelched"
)

// Sender delivers one event to one destination of a particular channel type.
// Implementations are stateless: the channel's stored config is passed in, so a
// new channel type never touches the scan pipeline (docs/NOTIFICATIONS.md).
type Sender interface {
	// Type is the channel type this sender handles, e.g. "ntfy".
	Type() string
	// Send delivers the event. A non-nil error means delivery failed.
	Send(ctx context.Context, cfg map[string]any, ev Event) error
}

// ConfigValidator is implemented by senders that can check their configuration
// before it is saved, so a bad URL or a missing topic is reported immediately
// rather than at 3 a.m.
type ConfigValidator interface {
	ValidateConfig(cfg map[string]any) error
}

// Registry maps channel types to senders.
type Registry struct {
	senders map[string]Sender
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{senders: map[string]Sender{}}
}

// Register adds a sender, replacing any previous one of the same type.
func (r *Registry) Register(s Sender) { r.senders[s.Type()] = s }

// Get returns the sender for a channel type.
func (r *Registry) Get(typ string) (Sender, bool) {
	s, ok := r.senders[typ]
	return s, ok
}

// Types lists the registered channel types, sorted.
func (r *Registry) Types() []string {
	out := make([]string, 0, len(r.senders))
	for typ := range r.senders {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

// ValidateConfig checks a config against the sender for its type.
func (r *Registry) ValidateConfig(typ string, cfg map[string]any) error {
	s, ok := r.senders[typ]
	if !ok {
		return fmt.Errorf("unsupported channel type %q", typ)
	}
	v, ok := s.(ConfigValidator)
	if !ok {
		return nil
	}
	return v.ValidateConfig(cfg)
}

// Store is the persistence the dispatcher needs.
type Store interface {
	ListChannels(ctx context.Context) ([]store.Channel, error)
	LogNotification(ctx context.Context, channelID int64, eventType, eventKey string, at time.Time, status, detail string) error
	LastNotificationAt(ctx context.Context, channelID int64, eventKey string) (*time.Time, error)
	GetSettings(ctx context.Context) (map[string]string, error)
}

// DispatcherOptions configures a Dispatcher.
type DispatcherOptions struct {
	Logger *slog.Logger
	// Budget caps the whole post-scan pass (default 30s) so a hung endpoint
	// cannot delay the next scheduled scan.
	Budget time.Duration
	// Squelch is how long a repeating condition stays quiet (default 1h).
	Squelch time.Duration
	// Location is the clock recurring maintenance windows are read against
	// (default time.Local, matching how the scheduler evaluates cron).
	Location *time.Location
	// Now overrides the clock in tests.
	Now func() time.Time
}

// Dispatcher fans events out to channels, applying subscription filters and
// squelching, and records every outcome in notification_log.
type Dispatcher struct {
	store   Store
	reg     *Registry
	log     *slog.Logger
	budget  time.Duration
	squelch time.Duration
	loc     *time.Location
	now     func() time.Time
}

// NewDispatcher builds a dispatcher over the given registry.
func NewDispatcher(st Store, reg *Registry, opts DispatcherOptions) *Dispatcher {
	d := &Dispatcher{
		store:   st,
		reg:     reg,
		log:     opts.Logger,
		budget:  opts.Budget,
		squelch: opts.Squelch,
		loc:     opts.Location,
		now:     opts.Now,
	}
	if d.log == nil {
		d.log = slog.Default()
	}
	if d.budget <= 0 {
		d.budget = 30 * time.Second
	}
	if d.squelch <= 0 {
		d.squelch = time.Hour
	}
	if d.loc == nil {
		d.loc = time.Local
	}
	if d.now == nil {
		d.now = time.Now
	}
	return d
}

// Dispatch delivers every event to every subscribed, enabled channel. It never
// returns an error: notifications must not be able to fail a scan.
func (d *Dispatcher) Dispatch(ctx context.Context, events []Event) {
	if len(events) == 0 {
		return
	}
	channels, err := d.store.ListChannels(ctx)
	if err != nil {
		d.log.Error("could not list notification channels", "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, d.budget)
	defer cancel()

	// Resolved once per pass rather than per delivery, so the settings table is read
	// at most once however many events and channels there are.
	p := d.newPass(ctx)

	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		sender, ok := d.reg.Get(ch.Type)
		if !ok {
			d.log.Warn("unknown notification channel type", "type", ch.Type, "channel", ch.ID)
			continue
		}
		cfg, cfgErr := decodeConfig(ch.Label, ch.Config)
		if cfgErr != nil {
			// The channel's configuration is unusable, so every event it subscribes to would
			// fail. Record that per event rather than sending nothing and saying nothing.
			for _, ev := range events {
				if subscribed(ch.Events, ev.Type) {
					d.record(ctx, ch.ID, ev, OutcomeFailed, cfgErr.Error())
				}
			}
			continue
		}
		for _, ev := range events {
			if !subscribed(ch.Events, ev.Type) {
				continue
			}
			d.deliver(ctx, ch, sender, cfg, ev, p)
		}
	}
}

// pass carries the configuration resolved once per dispatch pass.
type pass struct {
	squelch time.Duration
	windows []MaintenanceWindow
}

// newPass resolves the per-pass configuration from settings, falling back to the
// configured defaults when the table cannot be read.
func (d *Dispatcher) newPass(ctx context.Context) pass {
	p := pass{squelch: d.squelch}
	settings, err := d.store.GetSettings(ctx)
	if err != nil {
		return p
	}
	if seconds, perr := strconv.Atoi(strings.TrimSpace(settings["squelch_seconds"])); perr == nil && seconds > 0 {
		p.squelch = time.Duration(seconds) * time.Second
	}
	p.windows = ParseMaintenanceWindows(settings["maintenance_windows"])
	return p
}

// SendTest delivers a synthetic event to one channel, bypassing subscription
// filters and squelching (docs/NOTIFICATIONS.md). It reports the real outcome so
// the UI can show *why* a channel is not working.
//
// A channel with no id is an unsaved draft straight from the editor. It is still sent,
// but not recorded: there is no stored channel to attribute a delivery-log row to, and
// testing a draft must not write anything — that is the point of testing it.
func (d *Dispatcher) SendTest(ctx context.Context, ch store.Channel, ev Event) (Outcome, error) {
	sender, ok := d.reg.Get(ch.Type)
	if !ok {
		return OutcomeFailed, fmt.Errorf("unsupported channel type %q", ch.Type)
	}
	ctx, cancel := context.WithTimeout(ctx, d.budget)
	defer cancel()

	cfg, err := decodeConfig(ch.Label, ch.Config)
	if err != nil {
		if ch.ID != 0 {
			d.record(ctx, ch.ID, ev, OutcomeFailed, err.Error())
		}
		return OutcomeFailed, err
	}
	if err := sender.Send(ctx, cfg, ev); err != nil {
		if ch.ID != 0 {
			d.record(ctx, ch.ID, ev, OutcomeFailed, err.Error())
		}
		return OutcomeFailed, err
	}
	if ch.ID != 0 {
		d.record(ctx, ch.ID, ev, OutcomeSent, "test event")
	}
	return OutcomeSent, nil
}

// ValidateConfig checks a channel's config without saving it.
func (d *Dispatcher) ValidateConfig(typ string, cfg map[string]any) error {
	return d.reg.ValidateConfig(typ, cfg)
}

// Types lists the channel types this dispatcher can deliver to.
func (d *Dispatcher) Types() []string { return d.reg.Types() }

func (d *Dispatcher) deliver(ctx context.Context, ch store.Channel, sender Sender, cfg map[string]any, ev Event, p pass) {
	// A maintenance window silences everything for the drives it covers. It is
	// checked before squelching because it is a deliberate operator action, and the
	// delivery log should say so rather than blaming a squelch timer.
	if w := ActiveWindow(p.windows, d.now(), ev.Serial, d.loc); w != nil {
		d.record(ctx, ch.ID, ev, OutcomeSquelched, "maintenance window: "+w.Name())
		return
	}
	if window := squelchWindow(ev, p.squelch); window > 0 {
		last, err := d.store.LastNotificationAt(ctx, ch.ID, ev.Key())
		if err == nil && last != nil && d.now().Sub(*last) < window {
			d.record(ctx, ch.ID, ev, OutcomeSquelched, "within "+window.String()+" squelch window")
			return
		}
	}
	if err := sender.Send(ctx, cfg, ev); err != nil {
		d.log.Warn("notification delivery failed",
			"channel", ch.ID, "type", ch.Type, "event", ev.Type, "err", err)
		d.record(ctx, ch.ID, ev, OutcomeFailed, err.Error())
		return
	}
	d.log.Info("notification sent", "channel", ch.ID, "type", ch.Type, "event", ev.Type)
	d.record(ctx, ch.ID, ev, OutcomeSent, "")
}

// record writes a delivery outcome. The write deliberately survives context
// cancellation: the budget expiring must not erase the evidence of what happened.
func (d *Dispatcher) record(ctx context.Context, channelID int64, ev Event, outcome Outcome, detail string) {
	writeCtx := context.WithoutCancel(ctx)
	if err := d.store.LogNotification(writeCtx, channelID, ev.Type, ev.Key(), d.now(), string(outcome), detail); err != nil {
		d.log.Warn("could not record notification", "channel", channelID, "err", err)
	}
}

// squelchWindow returns how long a repeat of this event stays quiet. Zero means
// never, which is the right answer for most of the catalog: health transitions are
// deduped by the transition detection itself (ADR-008), appear/disappear are
// one-shot by construction, and a broken scanner is allowed to be loud.
func squelchWindow(ev Event, squelch time.Duration) time.Duration {
	// A hot drive and a hot chassis are both conditions rather than events: they persist until
	// somebody fixes them, so they stay quiet for the squelch window instead of repeating on every
	// scheduled scan. Everything else in the catalog is one-shot by construction or deduped by
	// transition detection (ADR-008).
	if ev.Type == EventTemperatureHigh || ev.Type == EventGroupHeat {
		return squelch
	}
	return 0
}

// subscribed reports whether a channel wants an event type. An empty or absent
// subscription list means "everything" — except for the opt-in events, which have to be
// named. An explicit "*" is still everything, including those.
func subscribed(eventsJSON, typ string) bool {
	var list []string
	if err := json.Unmarshal([]byte(eventsJSON), &list); err != nil {
		return !optInEvents[typ]
	}
	if len(list) == 0 {
		return !optInEvents[typ]
	}
	for _, t := range list {
		if t == typ || t == "*" {
			return true
		}
	}
	return false
}

// decodeConfig turns a stored channel config into a map, tolerating garbage, with any
// environment or `*_FILE` overrides applied (`secrets.go`).
//
// Garbage in the stored JSON is still tolerated: a corrupt row must not break delivery for
// every other channel. A broken *override* is not tolerated. An unreadable mounted secret is
// a deployment mistake, and quietly falling back to the stored value would send the wrong
// credential while the operator believed they had rotated it.
func decodeConfig(label, raw string) (map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return map[string]any{}, nil
		}
	}
	if err := ApplyOverrides(label, out); err != nil {
		return out, err
	}
	return out, nil
}
