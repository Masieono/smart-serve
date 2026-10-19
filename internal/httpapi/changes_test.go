package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

func attrsJSON(t *testing.T, attrs ...model.Attribute) string {
	t.Helper()
	blob, err := json.Marshal(storedAttributes{Attributes: attrs})
	if err != nil {
		t.Fatalf("marshal attributes: %v", err)
	}
	return string(blob)
}

func attr(id int, name string, value, threshold int, raw uint64) model.Attribute {
	return model.Attribute{
		ID: id, Name: name, Value: value, Worst: value, Threshold: threshold, RawValue: raw,
	}
}

func intOf(v int) *int { return &v }

func intPtr64(v int64) *int64 { return &v }

// An attribute that moved because it *should* move is routine wear, not news. Power-on hours go up
// every scan, and reporting that as a change would bury the one counter that matters.
func TestARoutineCounterIsCountedNotListed(t *testing.T) {
	prev := attrsJSON(t, attr(9, "Power_On_Hours", 99, 0, 31603))
	cur := attrsJSON(t, attr(9, "Power_On_Hours", 99, 0, 31627))

	trouble, routine := diffAttributes(prev, cur)
	if len(trouble) != 0 {
		t.Errorf("trouble = %+v, want none for a routine counter", trouble)
	}
	// Counted rather than dropped: a silent filter would be a lie by omission.
	if routine != 1 {
		t.Errorf("routine = %d, want 1", routine)
	}
}

// The lost-storage counters are the ones worth waking up for, and they are recognised by the same
// list the interpretation already sums.
func TestASectorCounterIsATrouble(t *testing.T) {
	prev := attrsJSON(t, attr(5, "Reallocated_Sector_Ct", 100, 10, 0))
	cur := attrsJSON(t, attr(5, "Reallocated_Sector_Ct", 100, 10, 5))

	trouble, routine := diffAttributes(prev, cur)
	if routine != 0 {
		t.Errorf("routine = %d, want 0", routine)
	}
	if len(trouble) != 1 {
		t.Fatalf("trouble = %+v, want one entry", trouble)
	}
	if trouble[0].ID != 5 || trouble[0].From != 0 || trouble[0].To != 5 {
		t.Errorf("entry = %+v", trouble[0])
	}
	if !trouble[0].Trouble {
		t.Error("a moved sector counter must be marked as trouble")
	}
	if trouble[0].Appeared {
		t.Error("the counter existed before; it only moved")
	}
}

// An attribute at or below the drive's *own* threshold is a finding whatever it is. That is the
// same rule the attribute table uses, and the same one the interpretation uses.
func TestAnAttributeAtItsOwnThresholdIsATrouble(t *testing.T) {
	prev := attrsJSON(t, attr(187, "Reported_Uncorrect", 100, 0, 0))
	cur := attrsJSON(t, attr(187, "Reported_Uncorrect", 1, 10, 1))

	trouble, _ := diffAttributes(prev, cur)
	if len(trouble) != 1 || !trouble[0].Trouble {
		t.Fatalf("trouble = %+v, want the threshold breach listed", trouble)
	}
}

// A threshold of 0 means the attribute has no threshold, not that it fails at zero — which is what
// most informational attributes report.
func TestAZeroThresholdIsNotABreach(t *testing.T) {
	prev := attrsJSON(t, attr(9, "Power_On_Hours", 100, 0, 1))
	cur := attrsJSON(t, attr(9, "Power_On_Hours", 0, 0, 2))

	trouble, routine := diffAttributes(prev, cur)
	if len(trouble) != 0 || routine != 1 {
		t.Errorf("got %d trouble and %d routine, want 0 and 1", len(trouble), routine)
	}
}

// A counter the drive has just started reporting is news whatever its value, and saying so is the
// difference between "nothing was wrong before" and "we were not being told".
func TestAnAppearedCounterIsMarked(t *testing.T) {
	trouble, _ := diffAttributes(attrsJSON(t), attrsJSON(t, attr(5, "Reallocated_Sector_Ct", 100, 10, 1)))
	if len(trouble) != 1 {
		t.Fatalf("trouble = %+v, want one entry", trouble)
	}
	if !trouble[0].Appeared {
		t.Error("Appeared = false, but the counter was not in the previous scan")
	}
	if trouble[0].From != 0 {
		t.Errorf("From = %d, want 0", trouble[0].From)
	}
}

// Worst first, so the counter closest to its threshold is the one you read. Ties break on the ID so
// the order does not shuffle between renders.
func TestTroubleAttributesAreOrderedWorstFirst(t *testing.T) {
	prev := attrsJSON(t,
		attr(5, "Reallocated_Sector_Ct", 100, 10, 0),
		attr(197, "Current_Pending_Sector", 100, 10, 0),
	)
	cur := attrsJSON(t,
		attr(5, "Reallocated_Sector_Ct", 95, 10, 1),
		attr(197, "Current_Pending_Sector", 100, 10, 1),
	)

	trouble, _ := diffAttributes(prev, cur)
	if len(trouble) != 2 {
		t.Fatalf("trouble = %+v, want two entries", trouble)
	}
	if trouble[0].ID != 5 {
		t.Errorf("first entry = %d, want the lower normalised value (5)", trouble[0].ID)
	}
}

// A blob that cannot be read must not fail the page: the rest of the diff is still true.
func TestAnUnreadableAttributesBlobIsNotFatal(t *testing.T) {
	trouble, routine := diffAttributes("{ not json", "{ also not json")
	if len(trouble) != 0 || routine != 0 {
		t.Errorf("got %d trouble and %d routine, want none", len(trouble), routine)
	}
}

// The first scan has nothing to compare against, and saying so beats a page of zero changes that
// reads as "nothing happened".
func TestDiffNeedsTwoScans(t *testing.T) {
	if got := diffSnapshots(nil, nil); got != nil {
		t.Fatalf("diffSnapshots(nil, nil) = %+v, want nil", got)
	}

	got := diffSnapshots(nil, &store.Snapshot{Health: "good"})
	if got == nil {
		t.Fatal("diffSnapshots(nil, cur) = nil, want a note")
	}
	if got.Note == "" {
		t.Error("no note explaining why there is no diff")
	}
	if got.Health != nil || got.Score != nil {
		t.Errorf("a first scan must report no changes, got %+v", got)
	}
}

// A drive scanned twice and found identical produces no changes at all. The UI states that in
// words, so the absence has to be exactly that: no fields, no attributes, no routine count.
func TestAnUnchangedDriveReportsNoChanges(t *testing.T) {
	blob := attrsJSON(t,
		attr(5, "Reallocated_Sector_Ct", 100, 10, 0),
		attr(9, "Power_On_Hours", 99, 0, 31603),
	)
	prev := &store.Snapshot{Health: "good", Score: 100, Attributes: blob}
	cur := &store.Snapshot{Health: "good", Score: 100, Attributes: blob}

	got := diffSnapshots(prev, cur)
	if got.Health != nil || got.Score != nil || got.TemperatureC != nil ||
		got.WearPct != nil || got.Sectors != nil {
		t.Errorf("expected no scalar changes, got %+v", got)
	}
	if len(got.Attributes) != 0 || got.Routine != 0 {
		t.Errorf("expected no attribute changes, got %d trouble and %d routine",
			len(got.Attributes), got.Routine)
	}
	if got.Note != "" {
		t.Errorf("Note = %q, but there is a diff — it is simply empty", got.Note)
	}
}

// An absent reading is not zero. A drive that reported no temperature last scan has no "from", and
// rendering one as 0 °C would invent a reading the drive never gave.
func TestAnAbsentReadingIsNotZero(t *testing.T) {
	got := diffSnapshots(
		&store.Snapshot{Health: "good"},
		&store.Snapshot{Health: "good", TemperatureC: intOf(41)},
	)
	if got.TemperatureC == nil {
		t.Fatal("TemperatureC = nil, want a change from absent to 41")
	}
	if got.TemperatureC.From != nil {
		t.Errorf("From = %v, want nil for a reading that was never taken", got.TemperatureC.From)
	}
	if got.TemperatureC.To != 41 {
		t.Errorf("To = %v, want 41", got.TemperatureC.To)
	}
}

// A reading that stopped being reported is the same finding in reverse: the "to" is absent, and
// that is not a drive sitting at zero.
func TestAReadingThatStoppedBeingReportedIsAbsent(t *testing.T) {
	got := diffSnapshots(
		&store.Snapshot{Health: "good", TemperatureC: intOf(41)},
		&store.Snapshot{Health: "good"},
	)
	if got.TemperatureC == nil || got.TemperatureC.To != nil {
		t.Fatalf("TemperatureC = %+v, want a change to absent", got.TemperatureC)
	}
}

func TestScalarChangesAreReported(t *testing.T) {
	got := diffSnapshots(
		&store.Snapshot{Health: "good", Score: 96, TemperatureC: intOf(33), Sectors: intPtr64(0)},
		&store.Snapshot{Health: "caution", Score: 74, TemperatureC: intOf(41), Sectors: intPtr64(5)},
	)
	if got.Health == nil || got.Health.From != "good" || got.Health.To != "caution" {
		t.Errorf("Health = %+v", got.Health)
	}
	if got.Score == nil || got.Score.From != 96 || got.Score.To != 74 {
		t.Errorf("Score = %+v", got.Score)
	}
	if got.Sectors == nil || got.Sectors.From != int64(0) || got.Sectors.To != int64(5) {
		t.Errorf("Sectors = %+v", got.Sectors)
	}
}
