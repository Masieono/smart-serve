package notify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/store"
)

var scanTime = time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)

type stateOpt func(*store.DeviceState)

func withName(name string) stateOpt {
	return func(s *store.DeviceState) { s.Name = name }
}

func withTemp(c int) stateOpt {
	return func(s *store.DeviceState) { s.TemperatureC = &c }
}

func asMissing() stateOpt {
	return func(s *store.DeviceState) { s.Missing = true }
}

func state(serial, health string, opts ...stateOpt) store.DeviceState {
	st := store.DeviceState{Serial: serial, Model: "Samsung SSD 870 EVO", Health: health}
	for _, o := range opts {
		o(&st)
	}
	return st
}

func states(list ...store.DeviceState) map[string]store.DeviceState {
	out := make(map[string]store.DeviceState, len(list))
	for _, s := range list {
		out[s.Serial] = s
	}
	return out
}

// withSelfTest attaches a stored self-test result, and says whether the drive has been read at
// all — the two are independent, which is the point of the flag.
func withSelfTest(e *model.SelfTestEntry, recorded bool) stateOpt {
	return func(s *store.DeviceState) {
		s.SelfTest = e
		s.SelfTestRecorded = recorded
	}
}

func withTags(tags string) stateOpt {
	return func(s *store.DeviceState) { s.Tags = tags }
}

// withAck records the verdict the user acknowledged (`FEATURES.md` B7).
func withAck(health string) stateOpt {
	return func(s *store.DeviceState) { s.AckHealth = health }
}

// B7: an acknowledged condition is one the user has said they know about. It silences the two alerts
// that describe the *condition* — a verdict change and a hot drive — and leaves the verdict alone:
// the drive still reads as caution on the dashboard, because it is caution.
func TestAnAcknowledgedConditionDoesNotAlert(t *testing.T) {
	rep := scan.PostScan{
		OK: true, Started: scanTime, Ended: scanTime,
		Previous: states(state("a", "caution", withAck("caution"), withTemp(60))),
		Current:  states(state("a", "caution", withAck("caution"), withTemp(61))),
	}
	events := Collect(rep, CollectOptions{TemperatureThresholdC: 55, Version: "test"})
	if len(events) != 0 {
		t.Errorf("an acknowledged condition produced events: %+v", events)
	}
}

// The ack must never hide a drive getting *worse* — that is the one thing it cannot do. It is spent
// the moment the verdict moves, because `AckHealth == cur.Health` stops being true.
func TestAWorseVerdictAlertsThroughAnAcknowledgement(t *testing.T) {
	rep := scan.PostScan{
		OK: true, Started: scanTime, Ended: scanTime,
		Previous: states(state("a", "caution", withAck("caution"))),
		Current:  states(state("a", "bad", withAck("caution"), withTemp(40))),
	}
	ev := onlyEvent(t, Collect(rep, CollectOptions{Version: "test"}), EventHealthChange)
	if ev.NewHealth != "bad" {
		t.Errorf("event = %+v, want the move to bad", ev)
	}
}

// And a drive that improves *back into* the acknowledged verdict is quiet again: the user
// acknowledged that condition, and it is the condition the drive is in.
func TestReturningToTheAcknowledgedVerdictIsQuiet(t *testing.T) {
	rep := scan.PostScan{
		OK: true, Started: scanTime, Ended: scanTime,
		Previous: states(state("a", "bad", withAck("caution"))),
		Current:  states(state("a", "caution", withAck("caution"), withTemp(40))),
	}
	if events := Collect(rep, CollectOptions{Version: "test"}); len(events) != 0 {
		t.Errorf("returning to the acknowledged verdict produced events: %+v", events)
	}
}

// An acknowledgement covers a condition, not a drive. Two things are still news while one is set:
// a drive that vanishes, and a self-test the user deliberately asked for.
func TestAnAcknowledgementDoesNotSilenceEverything(t *testing.T) {
	// Gone, while acknowledged.
	rep := scan.PostScan{
		OK: true, Started: scanTime, Ended: scanTime,
		Previous: states(state("a", "caution", withAck("caution"))),
		Current:  states(state("a", "caution", withAck("caution"), asMissing())),
	}
	onlyEvent(t, Collect(rep, CollectOptions{Version: "test"}), EventDeviceDisappeared)

	// A finished self-test, while acknowledged. A test is something the user started, so its result
	// is news whatever they have acknowledged about the drive's condition.
	passed := true
	before, after := int64(100), int64(200)
	rep = scan.PostScan{
		OK: true, Started: scanTime, Ended: scanTime,
		Previous: states(state("a", "caution", withAck("caution"),
			withSelfTest(&model.SelfTestEntry{Type: "Short", Hours: &before}, true))),
		Current: states(state("a", "caution", withAck("caution"),
			withSelfTest(&model.SelfTestEntry{Type: "Short", Hours: &after, Passed: &passed}, true))),
	}
	onlyEvent(t, Collect(rep, CollectOptions{Version: "test"}), EventSelfTestResult)
}

// A25: **one** event for the group rather than one per drive. Eight drives in a chassis have one fan,
// and eight alerts about one fan is precisely the noise that teaches people to ignore alerts.
func TestCollectReportsAGroupThatMovedTogether(t *testing.T) {
	rep := scan.PostScan{
		OK:      true,
		Started: scanTime, Ended: scanTime.Add(time.Second),
		Previous: states(
			state("a", "good", withTags("tank"), withTemp(35)),
			state("b", "good", withTags("tank"), withTemp(36)),
			state("c", "good", withTags("tank"), withTemp(34)),
			state("d", "good", withTags("tank"), withTemp(37)),
		),
		Current: states(
			state("a", "good", withTags("tank"), withTemp(46)),
			state("b", "good", withTags("tank"), withTemp(47)),
			state("c", "good", withTags("tank"), withTemp(45)),
			state("d", "good", withTags("tank"), withTemp(45)),
		),
	}

	events := Collect(rep, CollectOptions{
		Correlation: interp.DefaultCorrelationThresholds(),
		Version:     "test",
	})
	ev := onlyEvent(t, events, EventGroupHeat)

	if ev.Group != "tank" || ev.GroupSize != 4 || len(ev.GroupDrives) != 4 {
		t.Errorf("event = %+v, want the whole tank group", ev)
	}
	// The smallest rise among the movers, so the figure is true of every drive named.
	if ev.GroupDeltaC == nil || *ev.GroupDeltaC != 8 {
		t.Errorf("delta = %v, want 8", ev.GroupDeltaC)
	}
	// Four drives moved and one event was produced. That ratio is the whole feature.
	if n := len(events); n != 1 {
		t.Errorf("got %d events, want 1 — not one per drive: %+v", n, events)
	}
	// It states a correlation and never a cause: a RAID rebuild heats everything too.
	body := ev.Body()
	if !strings.Contains(body, "tank") || !strings.Contains(body, "8 °C") {
		t.Errorf("body does not name the group and the rise: %q", body)
	}
	if strings.Contains(strings.ToLower(body), "overheating") {
		t.Errorf("body claims a diagnosis: %q", body)
	}
}

// The correlation check is off when a caller has no settings, which is what a zero MinDrives means.
func TestCollectSkipsCorrelationWithoutThresholds(t *testing.T) {
	rep := scan.PostScan{
		OK: true, Started: scanTime, Ended: scanTime,
		Previous: states(state("a", "good", withTags("tank"), withTemp(35))),
		Current:  states(state("a", "good", withTags("tank"), withTemp(60))),
	}
	for _, ev := range Collect(rep, CollectOptions{Version: "test"}) {
		if ev.Type == EventGroupHeat {
			t.Errorf("a caller with no thresholds still got a finding: %+v", ev)
		}
	}
}

// A drive being tested is *expected* to heat up, and a batch of them on one bench would fire this on
// every scan (`FEATURES.md` D11).
func TestBenchDrivesAreNotPartOfACorrelation(t *testing.T) {
	previous := states(
		state("a", "good", withTags("bench, tank"), withTemp(35)),
		state("b", "good", withTags("bench, tank"), withTemp(36)),
		state("c", "good", withTags("bench, tank"), withTemp(34)),
		state("d", "good", withTags("bench, tank"), withTemp(37)),
	)
	current := states(
		state("a", "good", withTags("bench, tank"), withTemp(46)),
		state("b", "good", withTags("bench, tank"), withTemp(47)),
		state("c", "good", withTags("bench, tank"), withTemp(45)),
		state("d", "good", withTags("bench, tank"), withTemp(45)),
	)

	for _, ev := range Collect(scan.PostScan{
		OK: true, Started: scanTime, Ended: scanTime, Previous: previous, Current: current,
	}, CollectOptions{Correlation: interp.DefaultCorrelationThresholds(), Version: "test"}) {
		if ev.Type == EventGroupHeat {
			t.Errorf("a bench batch produced a finding: %+v", ev)
		}
	}
}

// It is **opt-in** (`ADR-023`), and that is the mitigation for the one thing that can go wrong here:
// a false "your enclosure is overheating" is worse than eight true per-drive alerts, so an install
// that would find it noisy must never receive it. An empty subscription list means "everything" for
// every other exception in the catalog, and deliberately not for this one.
func TestGroupHeatIsOptIn(t *testing.T) {
	if subscribed("", EventGroupHeat) {
		t.Error("an unsubscribed channel receives group_heat")
	}
	if subscribed(`["health_change"]`, EventGroupHeat) {
		t.Error("an unrelated subscription receives group_heat")
	}
	if !subscribed(`["group_heat"]`, EventGroupHeat) {
		t.Error("naming it does not subscribe")
	}
	if !subscribed(`["*"]`, EventGroupHeat) {
		t.Error("an explicit * should include it")
	}
}

// A hot chassis is a condition rather than an event: it persists until somebody fixes it, so it
// stays quiet for the squelch window instead of repeating on every scheduled scan.
func TestGroupHeatIsSquelchedPerGroup(t *testing.T) {
	if got := squelchWindow(Event{Type: EventGroupHeat}, time.Hour); got != time.Hour {
		t.Errorf("squelch = %v, want 1h", got)
	}
	// Per group, so a second enclosure heating up while the first is quiet is still news.
	if k := (Event{Type: EventGroupHeat, key: "group_heat:tank"}).Key(); k != "group_heat:tank" {
		t.Errorf("key = %q, want group_heat:tank", k)
	}
}

// onlyEvent returns the single event of a type, failing if there is not exactly one.
func onlyEvent(t *testing.T, events []Event, typ string) Event {
	t.Helper()
	var found []Event
	for _, e := range events {
		if e.Type == typ {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d %s events, want exactly 1 (all: %+v)", len(found), typ, events)
	}
	return found[0]
}

// noEvent fails if an event of a type was produced.
func noEvent(t *testing.T, events []Event, typ string) {
	t.Helper()
	for _, e := range events {
		if e.Type == typ {
			t.Fatalf("unexpected %s event: %+v", typ, e)
		}
	}
}

// report builds a successful *manual* scan over the two given state maps.
//
// Manual rather than scheduled so the device-event tests below are not also handed the
// scheduled-scan heartbeat. The trigger does not affect device-event generation, and mixing
// the two would make every assertion in this file a two-event assertion; the heartbeat has
// its own tests.
func report(previous, current map[string]store.DeviceState) scan.PostScan {
	return scan.PostScan{
		Trigger:  scan.Manual,
		ScanID:   1,
		Started:  scanTime,
		Ended:    scanTime,
		OK:       true,
		Previous: previous,
		Current:  current,
	}
}

// scheduledReport is report() for a scheduled scan, which is what the heartbeat keys off.
func scheduledReport(previous, current map[string]store.DeviceState) scan.PostScan {
	rep := report(previous, current)
	rep.Trigger = scan.Trigger{Kind: "schedule", ScheduleID: 7}
	return rep
}

// A scheduled scan that ran and found nothing to say is the only proof that monitoring is
// alive — and its *absence* is the thing you would want to notice.
func TestCollectScheduledScanHeartbeat(t *testing.T) {
	// No previous state and a healthy device, so the scan produces no other events and the
	// heartbeat is the only thing to send.
	rep := scheduledReport(nil, states(state("S1", "good")))
	rep.Ended = scanTime.Add(4200 * time.Millisecond)

	events := Collect(rep, CollectOptions{Version: "test"})
	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly one", events)
	}
	ev := events[0]
	if ev.Type != EventScanSuccess || ev.Priority != PriorityLow {
		t.Errorf("event = %+v, want a low-priority scan_success", ev)
	}
	if ev.DevicesTotal != 1 || ev.DurationMS != 4200 {
		t.Errorf("event = %+v, want the device count and the duration", ev)
	}
	if ev.Key() != "scan_success:7" {
		t.Errorf("key = %q, want scan_success:7", ev.Key())
	}
	// The body has to read on its own: most clients show it without the title header.
	if body := ev.Body(); !strings.Contains(body, "1 drive scanned in 4.2s") {
		t.Errorf("body = %q, want the summary line", body)
	}
}

// A scheduled scan that found something to say does not also get a heartbeat: the alert is
// the message. This is what makes the heartbeat the complement of the alert stream rather
// than an addition to it — you hear from a scheduled scan either way, and never twice.
func TestCollectNoHeartbeatWhenTheScanFoundSomething(t *testing.T) {
	rep := scheduledReport(
		states(state("S1", "good")),
		states(state("S1", "caution")),
	)
	events := Collect(rep, CollectOptions{Version: "test"})
	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly the health change", events)
	}
	if events[0].Type != EventHealthChange {
		t.Errorf("events[0] = %+v, want the health change and no heartbeat", events[0])
	}
}

// A manual scan does not get a heartbeat. Whoever pressed the button is watching the screen,
// and telling them the thing they just did happened is noise.
func TestCollectNoHeartbeatForAManualScan(t *testing.T) {
	rep := report(nil, states(state("S1", "good")))
	for _, ev := range Collect(rep, CollectOptions{}) {
		if ev.Type == EventScanSuccess {
			t.Errorf("manual scan produced a heartbeat: %+v", ev)
		}
	}
}

// The heartbeat is opt-in. "Everything" is the default subscription and it was designed
// around exceptions; a periodic all-clear must not arrive at every channel that never asked
// for it, least of all the moment the feature is added.
func TestHeartbeatIsOptIn(t *testing.T) {
	cases := map[string]bool{
		``:                                false,
		`[]`:                              false,
		`["health_change"]`:               false,
		`["scan_success"]`:                true,
		`["scan_success","scan_failure"]`: true,
		`["*"]`:                           true,
	}
	for events, want := range cases {
		if got := subscribed(events, EventScanSuccess); got != want {
			t.Errorf("subscribed(%s, scan_success) = %v, want %v", events, got, want)
		}
	}
	// Every other event keeps the old behaviour: an empty list still means everything.
	for _, typ := range []string{EventHealthChange, EventScanFailure, EventDeviceAppeared} {
		if !subscribed("", typ) {
			t.Errorf("an empty subscription must still mean everything for %s", typ)
		}
	}
}

func TestCollectHealthChange(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good", withName("boot"))),
		states(state("S1", "caution", withName("boot"))),
	), CollectOptions{TemperatureThresholdC: 55, Version: "test"})

	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly one", events)
	}
	ev := events[0]
	if ev.Type != EventHealthChange || ev.Priority != PriorityHigh {
		t.Errorf("event = %+v, want a high-priority health_change", ev)
	}
	if ev.OldHealth != "good" || ev.NewHealth != "caution" {
		t.Errorf("transition = %s → %s, want good → caution", ev.OldHealth, ev.NewHealth)
	}
	if ev.Serial != "S1" || ev.DeviceName != "boot" {
		t.Errorf("device = %+v", ev)
	}
	if ev.ScanTS != scanTime.UnixMilli() || ev.Version != "test" {
		t.Errorf("metadata = %+v", ev)
	}
	// The key encodes the *new* verdict, so a drive that recovers and degrades
	// again produces a distinct event rather than a squelched one.
	if ev.Key() != "health_change:S1:caution" {
		t.Errorf("key = %q", ev.Key())
	}
}

// ADR-008: a persistently bad drive must not alert on every scheduled scan.
func TestCollectIsSilentWhenNothingChanged(t *testing.T) {
	events := Collect(report(
		states(state("S1", "caution"), state("S2", "good")),
		states(state("S1", "caution"), state("S2", "good")),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 0 {
		t.Errorf("events = %+v, want none", events)
	}
}

func TestCollectDeviceDisappeared(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good"), state("S2", "good")),
		states(state("S1", "good"), state("S2", "good", asMissing())),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly one", events)
	}
	if events[0].Type != EventDeviceDisappeared || events[0].Serial != "S2" {
		t.Errorf("event = %+v, want S2 disappeared", events[0])
	}
	if events[0].Key() != "device_disappeared:S2" {
		t.Errorf("key = %q", events[0].Key())
	}
}

// A device already known to be missing must not be announced again.
func TestCollectDoesNotRepeatADisappearance(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good"), state("S2", "good", asMissing())),
		states(state("S1", "good"), state("S2", "good", asMissing())),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 0 {
		t.Errorf("events = %+v, want none", events)
	}
}

func TestCollectDeviceAppeared(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good")),
		states(state("S1", "good"), state("S2", "good", withName("backup"))),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly one", events)
	}
	if events[0].Type != EventDeviceAppeared || events[0].Serial != "S2" {
		t.Errorf("event = %+v, want S2 appeared", events[0])
	}
	if events[0].Priority != PriorityLow {
		t.Errorf("priority = %q, want low", events[0].Priority)
	}
}

// A replug is an appearance, not a health change: the stored verdict is stale, so
// comparing against it would be misleading.
func TestCollectReappearanceIsAnAppearance(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good", asMissing())),
		states(state("S1", "bad")),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 1 || events[0].Type != EventDeviceAppeared {
		t.Fatalf("events = %+v, want a single device_appeared", events)
	}
}

// The very first scan would otherwise announce every drive as "detected".
func TestCollectIsQuietOnTheFirstScan(t *testing.T) {
	events := Collect(report(nil,
		states(state("S1", "good"), state("S2", "caution")),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 0 {
		t.Errorf("events = %+v, want none on the first scan", events)
	}
}

func TestCollectTemperatureHigh(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good")),
		states(state("S1", "good", withTemp(61))),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly one", events)
	}
	ev := events[0]
	if ev.Type != EventTemperatureHigh || ev.Priority != PriorityMedium {
		t.Errorf("event = %+v", ev)
	}
	if ev.TemperatureC == nil || *ev.TemperatureC != 61 {
		t.Errorf("temperature = %v, want 61", ev.TemperatureC)
	}
	if ev.Key() != "temperature_high:S1" {
		t.Errorf("key = %q — it must be stable so the squelch window applies", ev.Key())
	}
}

func TestCollectTemperatureAtTheThreshold(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good")),
		states(state("S1", "good", withTemp(55))),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 1 || events[0].Type != EventTemperatureHigh {
		t.Errorf("events = %+v, want the threshold to be inclusive", events)
	}
}

// A drive that is gone cannot be "running hot": its temperature is the last value
// ever seen, so alerting on it would report history as if it were now.
func TestCollectIgnoresTheTemperatureOfAMissingDevice(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good", withTemp(70))),
		states(state("S1", "good", withTemp(70), asMissing())),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 1 || events[0].Type != EventDeviceDisappeared {
		t.Errorf("events = %+v, want only the disappearance", events)
	}
}

func TestCollectScanFailure(t *testing.T) {
	rep := scan.PostScan{
		Trigger: scan.Trigger{Kind: "schedule", ScheduleID: 7},
		Ended:   scanTime, OK: false, Error: "smartctl not found",
	}
	events := Collect(rep, CollectOptions{Version: "test"})

	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly one", events)
	}
	ev := events[0]
	if ev.Type != EventScanFailure || ev.Priority != PriorityMedium {
		t.Errorf("event = %+v", ev)
	}
	if ev.ScheduleID != 7 || ev.Error != "smartctl not found" {
		t.Errorf("event = %+v", ev)
	}
	if ev.Key() != "scan_failure:7" {
		t.Errorf("key = %q", ev.Key())
	}
}

// A manual scan that fails is already visible to whoever pressed the button.
func TestCollectIgnoresAManualScanFailure(t *testing.T) {
	rep := scan.PostScan{
		Trigger: scan.Trigger{Kind: "manual"},
		Ended:   scanTime, OK: false, Error: "boom",
	}
	if events := Collect(rep, CollectOptions{}); len(events) != 0 {
		t.Errorf("events = %+v, want none for a manual failure", events)
	}
}

func TestEventTitleAndBody(t *testing.T) {
	ev := Event{
		Type: EventHealthChange, Priority: PriorityHigh,
		DeviceName: "boot", DeviceModel: "Samsung SSD 870 EVO",
		OldHealth: "good", NewHealth: "caution",
		Rationale: "5 pending sectors", Serial: "S5GXNX0R512345J",
		ScanTS: scanTime.UnixMilli(),
	}

	if got, want := ev.Title(), "Drive health changed: boot (Samsung SSD 870 EVO)"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
	body := ev.Body()
	for _, want := range []string{"good → caution", "5 pending sectors", "Serial S5GXNX0R512345J"} {
		if !strings.Contains(body, want) {
			t.Errorf("Body = %q, missing %q", body, want)
		}
	}
}

// An ntfy Title header cannot carry emoji, so the header form must stay ASCII
// while the body keeps the full message.
func TestEventTitleIsASCII(t *testing.T) {
	ev := Event{Type: EventHealthChange, DeviceName: "bööt", NewHealth: "bad"}
	if got := ascii(ev.Title()); strings.ContainsRune(got, 'ö') {
		t.Errorf("ascii(%q) = %q, want non-ASCII stripped", ev.Title(), got)
	}
	if !strings.ContainsRune(ev.Body(), '🛑') {
		t.Errorf("Body = %q, want the emoji retained in the message", ev.Body())
	}
}

// A test notification has to read as a test. "Drive health changed" for a drive that
// does not exist is confusing in a notification tray — and in the JSON it is worse,
// because it can trip an automation that is watching for real alerts.
func TestTestEventIsIdentifiableAsATest(t *testing.T) {
	ev := TestEvent("1.2.3")

	if !ev.Test {
		t.Error("TestEvent must set Test, or nothing downstream can tell it apart")
	}
	if got, want := ev.Title(), "Test from Smart-Serve"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
	body := ev.Body()
	if !strings.Contains(body, "test notification") {
		t.Errorf("Body = %q, want it to say it is a test", body)
	}
	if !strings.Contains(body, "1.2.3") {
		t.Errorf("Body = %q, want the version, so an operator knows which instance sent it", body)
	}
	// The alert wording must not leak into the test message.
	if strings.Contains(body, "Drive health changed") {
		t.Errorf("Body = %q, want no drive-alert wording", body)
	}
}

// The flag has to be on the wire. A consumer that cannot see it has no way to decline
// to act on a test.
func TestTestEventSerialisesTheTestFlag(t *testing.T) {
	raw, err := json.Marshal(TestEvent("1.2.3"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload["test"] != true {
		t.Errorf("payload = %s, want \"test\": true", raw)
	}

	// A real alert must not carry it, or the flag means nothing.
	raw, err = json.Marshal(Event{Type: EventHealthChange, NewHealth: "bad"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), "\"test\"") {
		t.Errorf("real event payload = %s, want no test flag", raw)
	}
}

// The payload keeps a realistic shape, so a webhook consumer's parser is exercised by
// the test rather than only by a real alert.
func TestTestEventPayloadIsWellFormed(t *testing.T) {
	ev := TestEvent("1.2.3")
	if ev.Type != EventHealthChange || ev.Serial == "" || ev.ScanTS == 0 || ev.Version != "1.2.3" {
		t.Errorf("test event = %+v, want a complete payload", ev)
	}
}

func TestTemperatureAlertsDefaultToCelsius(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good")),
		states(state("S1", "good", withTemp(61))),
	), CollectOptions{TemperatureThresholdC: 55})

	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly one", events)
	}
	if !strings.Contains(events[0].Body(), "61 °C") {
		t.Errorf("Body = %q, want Celsius when no unit is configured", events[0].Body())
	}
}

// An alert must be worded in the unit the UI is showing, or the same drive reads
// as two different temperatures depending on where you look.
func TestTemperatureAlertsFollowTheDisplayUnit(t *testing.T) {
	events := Collect(report(
		states(state("S1", "good")),
		states(state("S1", "good", withTemp(61))),
	), CollectOptions{TemperatureThresholdC: 55, TemperatureUnit: "f"})

	if len(events) != 1 {
		t.Fatalf("events = %+v, want exactly one", events)
	}
	ev := events[0]
	if !strings.Contains(ev.Body(), "142 °F") { // 61 °C
		t.Errorf("Body = %q, want the reading in Fahrenheit", ev.Body())
	}
	if !strings.Contains(ev.Rationale, "131 °F") { // 55 °C
		t.Errorf("Rationale = %q, want the threshold in Fahrenheit too", ev.Rationale)
	}
	// The payload keeps canonical Celsius: a machine consumer must not have to
	// guess which unit it is looking at.
	if ev.TemperatureC == nil || *ev.TemperatureC != 61 {
		t.Errorf("TemperatureC = %v, want the canonical 61", ev.TemperatureC)
	}
}

// fakeSettings is an in-memory Settings source for the notifier.
type fakeSettings map[string]string

func (f fakeSettings) GetSettings(context.Context) (map[string]string, error) { return f, nil }

func TestNotifierReadsTemperatureSettings(t *testing.T) {
	st := newFakeStore(store.Channel{ID: 1, Type: "rec", Enabled: true, Events: "[]"})
	sender := &recordingSender{typ: "rec"}
	d := testDispatcher(st, sender, DispatcherOptions{Now: func() time.Time { return scanTime }})
	n := NewNotifier(fakeSettings{"temperature_threshold_c": "55", "temperature_unit": "f"}, d, "test", discardLogger())

	n.OnScan(context.Background(), report(
		states(state("S1", "good")),
		states(state("S1", "good", withTemp(61))),
	))

	if sender.count() != 1 {
		t.Fatalf("deliveries = %d, want 1", sender.count())
	}
	if body := sender.sent[0].Body(); !strings.Contains(body, "142 °F") {
		t.Errorf("Body = %q, want the configured Fahrenheit unit", body)
	}
}

func TestNotifierHonoursAConfiguredThreshold(t *testing.T) {
	st := newFakeStore(store.Channel{ID: 1, Type: "rec", Enabled: true, Events: "[]"})
	sender := &recordingSender{typ: "rec"}
	d := testDispatcher(st, sender, DispatcherOptions{Now: func() time.Time { return scanTime }})
	// A threshold of 35 °C makes a 40 °C drive alert, where the 55 °C default would not.
	n := NewNotifier(fakeSettings{"temperature_threshold_c": "35"}, d, "test", discardLogger())

	n.OnScan(context.Background(), report(
		states(state("S1", "good")),
		states(state("S1", "good", withTemp(40))),
	))

	if sender.count() != 1 {
		t.Fatalf("deliveries = %d, want 1 at the configured threshold", sender.count())
	}
	if sender.sent[0].Type != EventTemperatureHigh {
		t.Errorf("event = %+v, want a temperature_high", sender.sent[0])
	}
}

// A finished self-test is news, and a failed one is the whole reason to run a test — so the
// outcome decides the priority rather than the event type.
func TestSelfTestResultNotifiesWithTheDrivesOwnVerdict(t *testing.T) {
	hours := int64(2116)
	passed, failed := true, false

	base := func() store.DeviceState {
		return state("S1", "good", withSelfTest(&model.SelfTestEntry{
			Type: "Short offline", Hours: &hours, Passed: &passed, Status: "Completed without error",
		}, true))
	}
	after := state("S1", "good", withSelfTest(&model.SelfTestEntry{
		Type: "Extended offline", Hours: &hours, Passed: &failed, Status: "Completed: read failure",
	}, true))

	ev := onlyEvent(t, Collect(report(states(base()), states(after)), CollectOptions{}), EventSelfTestResult)

	if ev.Priority != PriorityHigh {
		t.Errorf("priority = %q, want high for a failure", ev.Priority)
	}
	if ev.SelfTestType != "Extended offline" {
		t.Errorf("SelfTestType = %q", ev.SelfTestType)
	}
	if ev.SelfTestPassed == nil || *ev.SelfTestPassed {
		t.Errorf("SelfTestPassed = %v, want false", ev.SelfTestPassed)
	}
	// The drive's own words, not a paraphrase: the body is the only part most people read.
	if !strings.Contains(ev.Rationale, "Completed: read failure") {
		t.Errorf("rationale = %q, want the drive's own status", ev.Rationale)
	}
}

func TestPassingSelfTestNotifiesAtLowPriority(t *testing.T) {
	hours := int64(10)
	passed := true

	ev := onlyEvent(t, Collect(report(
		states(state("S1", "good", withSelfTest(&model.SelfTestEntry{
			Type: "Short offline", Hours: &hours, Passed: &passed,
		}, true))),
		states(state("S1", "good", withSelfTest(&model.SelfTestEntry{
			Type: "Extended offline", Hours: &hours, Passed: &passed, Status: "Completed without error",
		}, true))),
	), CollectOptions{}), EventSelfTestResult)

	if ev.Priority != PriorityLow {
		t.Errorf("priority = %q, want low for a pass", ev.Priority)
	}
}

// The same result re-read is not a new result. Without this the event would fire on every scan
// for as long as the drive kept the entry, which on a 15-minute schedule is 96 messages a day.
func TestTheSameSelfTestResultDoesNotNotifyTwice(t *testing.T) {
	hours := int64(2116)
	passed := true
	result := func() *model.SelfTestEntry {
		return &model.SelfTestEntry{
			Type: "Extended offline", Hours: &hours, Passed: &passed, Status: "Completed without error",
		}
	}

	events := Collect(report(
		states(state("S1", "good", withSelfTest(result(), true))),
		states(state("S1", "good", withSelfTest(result(), true))),
	), CollectOptions{})

	noEvent(t, events, EventSelfTestResult)
}

// The upgrade case, and the reason the flag exists at all. A row the app has never read has no
// baseline, so the first scan after upgrading must record what it finds rather than announce it.
// Every drive with any history would otherwise fire at once, on the first scan after the update.
func TestADriveWithNoRecordedBaselineAnnouncesNothing(t *testing.T) {
	hours := int64(2116)
	passed := true

	events := Collect(report(
		states(state("S1", "good")),
		states(state("S1", "good", withSelfTest(&model.SelfTestEntry{
			Type: "Extended offline", Hours: &hours, Passed: &passed,
		}, true))),
	), CollectOptions{})

	noEvent(t, events, EventSelfTestResult)
}

// The counterpart: a log that *was* read and held nothing is a real baseline, so the first test to
// finish against it is news.
func TestFirstResultAgainstARecordedEmptyLogNotifies(t *testing.T) {
	hours := int64(5)
	passed := true

	events := Collect(report(
		states(state("S1", "good", withSelfTest(nil, true))),
		states(state("S1", "good", withSelfTest(&model.SelfTestEntry{
			Type: "Short offline", Hours: &hours, Passed: &passed,
		}, true))),
	), CollectOptions{})

	onlyEvent(t, events, EventSelfTestResult)
}

// A drive that has vanished cannot have finished a test, and its stored result is the last one
// ever seen. Reporting it would be history dressed up as news — the same rule the temperature
// check follows.
func TestAMissingDriveDoesNotReportASelfTestResult(t *testing.T) {
	hours := int64(2116)
	passed := true
	stored := &model.SelfTestEntry{Type: "Short offline", Hours: &hours, Passed: &passed}

	events := Collect(report(
		states(state("S1", "good", withSelfTest(stored, true))),
		states(state("S1", "good", asMissing(), withSelfTest(stored, true))),
	), CollectOptions{})

	noEvent(t, events, EventSelfTestResult)
}

// A drive that gave no verdict is not a drive that failed one. Treating the absent verdict as a
// failure would put a red cross on a result the drive simply did not characterise.
func TestASelfTestWithNoVerdictIsNotAFailure(t *testing.T) {
	hours := int64(7)

	ev := onlyEvent(t, Collect(report(
		states(state("S1", "good", withSelfTest(nil, true))),
		states(state("S1", "good", withSelfTest(&model.SelfTestEntry{
			Type: "Short offline", Hours: &hours, Status: "Completed",
		}, true))),
	), CollectOptions{}), EventSelfTestResult)

	if ev.Priority != PriorityLow {
		t.Errorf("priority = %q, want low when the drive gave no verdict", ev.Priority)
	}
	if ev.SelfTestPassed != nil {
		t.Errorf("SelfTestPassed = %v, want absent", ev.SelfTestPassed)
	}
}
