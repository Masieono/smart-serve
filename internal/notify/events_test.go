package notify

import (
	"strings"
	"testing"
	"time"

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

// report builds a successful scheduled scan over the two given state maps.
func report(previous, current map[string]store.DeviceState) scan.PostScan {
	return scan.PostScan{
		Trigger:  scan.Trigger{Kind: "schedule", ScheduleID: 7},
		ScanID:   1,
		Started:  scanTime,
		Ended:    scanTime,
		OK:       true,
		Previous: previous,
		Current:  current,
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

func TestTestEventLooksLikeARealAlert(t *testing.T) {
	ev := TestEvent("1.2.3")
	if ev.Type != EventHealthChange || ev.OldHealth != "good" || ev.NewHealth != "caution" {
		t.Errorf("test event = %+v", ev)
	}
	if ev.Version != "1.2.3" || ev.Body() == "" {
		t.Errorf("test event = %+v", ev)
	}
}
