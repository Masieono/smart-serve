package interp

import (
	"slices"
	"strings"
	"testing"
)

// reading is one drive's pair of temperatures, in one or more groups.
func reading(serial string, groups []string, prev, now int) HeatReading {
	r := HeatReading{Serial: serial, Groups: groups}
	if prev >= 0 {
		p := prev
		r.Prev = &p
	}
	if now >= 0 {
		n := now
		r.Now = &n
	}
	return r
}

func defaultT() CorrelationThresholds { return DefaultCorrelationThresholds() }

// The case the feature exists for: one chassis, one fan, four drives hotter at once.
func TestAGroupThatMovedTogetherIsOneFinding(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"tank"}, 35, 46),
		reading("b", []string{"tank"}, 36, 47),
		reading("c", []string{"tank"}, 34, 45),
		reading("d", []string{"tank"}, 37, 45),
	}

	got := CorrelatedHeat(readings, defaultT())
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(got), got)
	}
	if got[0].Group != "tank" || len(got[0].Drives) != 4 || got[0].Total != 4 {
		t.Errorf("finding = %+v, want 4 of 4 in tank", got[0])
	}
	// The *smallest* rise among the movers, so the figure is true of every drive named. A mean would
	// let one drive's spike stand in for the others.
	if got[0].DeltaC != 8 {
		t.Errorf("delta = %d, want 8 (the smallest rise)", got[0].DeltaC)
	}
	if got[0].MaxC == nil || *got[0].MaxC != 47 {
		t.Errorf("max = %v, want 47", got[0].MaxC)
	}
}

// A high bar is the whole design (ADR-023): a false "your enclosure is overheating" is worse than
// eight true per-drive alerts, so the rule says nothing until enough drives have moved.
func TestTooFewDrivesMovingIsNotAFinding(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"tank"}, 35, 46),
		reading("b", []string{"tank"}, 36, 47),
		reading("c", []string{"tank"}, 34, 35),
		reading("d", []string{"tank"}, 37, 37),
	}

	if got := CorrelatedHeat(readings, defaultT()); len(got) != 0 {
		t.Errorf("two of four moving produced a finding: %+v", got)
	}
}

// A group where every drive crept up by a degree or two is a warm afternoon, not a stopped fan.
func TestASmallRiseIsNotAFinding(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"tank"}, 35, 40),
		reading("b", []string{"tank"}, 36, 41),
		reading("c", []string{"tank"}, 34, 39),
		reading("d", []string{"tank"}, 37, 42),
	}

	if got := CorrelatedHeat(readings, defaultT()); len(got) != 0 {
		t.Errorf("a 5 °C rise produced a finding at a delta of 8: %+v", got)
	}
}

// A drop is not a rise. A group cooling down is not the finding this looks for.
func TestCoolingDownIsNotAFinding(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"tank"}, 46, 35),
		reading("b", []string{"tank"}, 47, 36),
		reading("c", []string{"tank"}, 45, 34),
		reading("d", []string{"tank"}, 45, 37),
	}

	if got := CorrelatedHeat(readings, defaultT()); len(got) != 0 {
		t.Errorf("a group cooling down produced a finding: %+v", got)
	}
}

// Untagged drives have no group, so they can never correlate — there is nothing for them to share.
func TestUntaggedDrivesAreNotAGroup(t *testing.T) {
	readings := []HeatReading{
		reading("a", nil, 35, 46),
		reading("b", nil, 36, 47),
		reading("c", nil, 34, 45),
		reading("d", nil, 37, 45),
	}

	if got := CorrelatedHeat(readings, defaultT()); len(got) != 0 {
		t.Errorf("untagged drives produced a finding: %+v", got)
	}
}

// The threshold is the caller's: raising the delta above what happened silences the finding, and
// lowering it produces one. The rule has no opinion about which is right.
func TestTheThresholdsAreTheCallers(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"tank"}, 35, 40),
		reading("b", []string{"tank"}, 36, 41),
		reading("c", []string{"tank"}, 34, 39),
	}

	if got := CorrelatedHeat(readings, CorrelationThresholds{MinDrives: 3, DeltaC: 4}); len(got) != 1 {
		t.Errorf("3 drives up 4 °C produced no finding: %+v", got)
	}
	// A MinDrives below two is not "off", it is "any one drive that moved" — and a DeltaC of zero
	// would call every drive that held steady a mover. Both are refused rather than obeyed, so a
	// typo cannot produce a wrong alert.
	if got := CorrelatedHeat(readings, CorrelationThresholds{MinDrives: 1, DeltaC: 4}); got != nil {
		t.Errorf("MinDrives of 1 was honoured: %+v", got)
	}
	if got := CorrelatedHeat(readings, CorrelationThresholds{MinDrives: 3, DeltaC: 0}); got != nil {
		t.Errorf("a zero delta was honoured: %+v", got)
	}
}

// A drive counts for each of its tags, the same rule as the grouping (D9): a group that hid a drive
// would be worse than one that reports it twice.
func TestADriveCountsForEveryGroupItIsIn(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"tank", "backup"}, 35, 46),
		reading("b", []string{"tank"}, 36, 47),
		reading("c", []string{"tank"}, 34, 45),
		reading("d", []string{"backup"}, 37, 45),
	}

	got := CorrelatedHeat(readings, CorrelationThresholds{MinDrives: 2, DeltaC: 8})
	if len(got) != 2 {
		t.Fatalf("got %d findings, want one per group: %+v", len(got), got)
	}
	// The shared drive is named in both, and each group's count is its own: `tank` has three drives
	// that moved, `backup` has two, and `a` is one of them either way.
	byGroup := map[string]GroupHeat{}
	for _, g := range got {
		byGroup[g.Group] = g
	}
	if n := len(byGroup["tank"].Drives); n != 3 {
		t.Errorf("tank = %v, want three drives", byGroup["tank"].Drives)
	}
	if n := len(byGroup["backup"].Drives); n != 2 {
		t.Errorf("backup = %v, want two drives", byGroup["backup"].Drives)
	}
	if !slices.Contains(byGroup["tank"].Drives, "a") || !slices.Contains(byGroup["backup"].Drives, "a") {
		t.Error("the drive in both tags is missing from one of the groups")
	}
}

// A drive with no earlier reading belongs to its group — it is part of the ratio — but it cannot be
// one of the drives that moved. Dropping it would silently shrink the denominator and make a
// two-of-three read as a two-of-two.
func TestADriveWithNoPreviousReadingStaysInTheRatio(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"tank"}, 35, 46),
		reading("b", []string{"tank"}, 36, 47),
		reading("c", []string{"tank"}, -1, 40), // the first time we have seen it
	}

	got := CorrelatedHeat(readings, CorrelationThresholds{MinDrives: 2, DeltaC: 8})
	if len(got) != 1 {
		t.Fatalf("got %+v, want one finding", got)
	}
	if got[0].Total != 3 {
		t.Errorf("total = %d, want 3 — the new drive is still in the group", got[0].Total)
	}
	if len(got[0].Drives) != 2 {
		t.Errorf("drives = %v, want the two that moved", got[0].Drives)
	}
}

// A drive that reported no temperature is not a drive that held steady, so it is not in the
// denominator either — otherwise a group with three silent drives could never reach any threshold.
func TestADriveWithNoTemperatureIsNotCounted(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"tank"}, 35, 46),
		reading("b", []string{"tank"}, 36, 47),
		reading("silent", []string{"tank"}, -1, -1),
	}

	got := CorrelatedHeat(readings, CorrelationThresholds{MinDrives: 2, DeltaC: 8})
	if len(got) != 1 || got[0].Total != 2 {
		t.Errorf("got %+v, want 2 of 2 — the silent drive is not a member", got)
	}
}

// More drives moving is the stronger finding, so it leads. Ties break on the shared rise and then on
// the name, so the list is stable between renders.
func TestFindingsAreOrderedByHowMuchMoved(t *testing.T) {
	readings := []HeatReading{
		reading("a", []string{"media"}, 30, 39),
		reading("b", []string{"media"}, 30, 39),
		reading("c", []string{"tank"}, 30, 45),
		reading("d", []string{"tank"}, 30, 45),
		reading("e", []string{"tank"}, 30, 45),
	}

	got := CorrelatedHeat(readings, CorrelationThresholds{MinDrives: 2, DeltaC: 8})
	if len(got) != 2 {
		t.Fatalf("got %+v, want two findings", got)
	}
	if got[0].Group != "tank" || got[1].Group != "media" {
		t.Errorf("order = %s then %s, want tank (3 drives) first", got[0].Group, got[1].Group)
	}
}

// The wording is the mitigation for the one thing that can go wrong here (ADR-023): it states a
// correlation and where to look, and never a cause. A RAID rebuild heats everything too, and the
// reader knows whether one is running.
func TestTheSummaryStatesACorrelationRatherThanACause(t *testing.T) {
	g := GroupHeat{Group: "tank", Drives: []string{"a", "b", "c", "d"}, Total: 4, DeltaC: 8}

	got := HeatSummary(g, "8 °C")
	for _, want := range []string{"4 of the 4", "tank", "8 °C", "more likely to be there"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q is missing %q", got, want)
		}
	}
	// It must not claim to know what is wrong.
	for _, unwanted := range []string{"overheating", "failed", "dying"} {
		if strings.Contains(strings.ToLower(got), unwanted) {
			t.Errorf("summary claims a diagnosis (%q): %s", unwanted, got)
		}
	}

	// A partial group is worded as partial: "most of them share an enclosure" rather than "they".
	partial := HeatSummary(GroupHeat{Group: "tank", Drives: []string{"a", "b"}, Total: 9, DeltaC: 8}, "8 °C")
	if !strings.Contains(partial, "2 of the 9") || !strings.Contains(partial, "most of them") {
		t.Errorf("a partial group is worded as total: %s", partial)
	}
}

// The settings are the owner's two numbers (ADR-023), with defaults chosen for a small enclosure.
func TestCorrelationSettingsFallBackToTheDefaults(t *testing.T) {
	if got := CorrelationFromSettings(nil); got != DefaultCorrelationThresholds() {
		t.Errorf("nil settings = %+v, want the defaults", got)
	}
	if got := CorrelationFromSettings(map[string]string{"correlation_min_drives": "12"}); got.MinDrives != 12 {
		t.Errorf("min drives = %d, want 12", got.MinDrives)
	}
	if got := CorrelationFromSettings(map[string]string{"correlation_delta_c": "4"}); got.DeltaC != 4 {
		t.Errorf("delta = %d, want 4", got.DeltaC)
	}

	// A typo must not turn the check into something else: zero min-drives would mean "any one drive
	// that moved", and a zero delta would call every steady drive a mover.
	for _, bad := range []map[string]string{
		{"correlation_min_drives": "0"},
		{"correlation_min_drives": "one"},
		{"correlation_delta_c": "0"},
		{"correlation_delta_c": "-5"},
		{"correlation_delta_c": "eight"},
	} {
		if got := CorrelationFromSettings(bad); got != DefaultCorrelationThresholds() {
			t.Errorf("%v = %+v, want the defaults", bad, got)
		}
	}
}
