package interp

import (
	"testing"

	"github.com/masieono/smart-serve/internal/model"
)

// `FEATURES.md` A12: the breakdown has to name the attribute each line came from, so the table can
// mark the rows the verdict actually read. Without an id the only way to mark a row is to guess from
// its threshold, which is the drive's opinion rather than this app's.
func TestATAPenaltiesCarryTheAttributeTheyCameFrom(t *testing.T) {
	sd := &model.SmartData{
		Bus: model.BusATA,
		Attributes: []model.Attribute{
			{ID: 5, Name: "Reallocated_Sector_Ct", Value: 90, RawValue: 12},
			{ID: 197, Name: "Current_Pending_Sector", Value: 90, RawValue: 4},
			{ID: 199, Name: "UDMA_CRC_Error_Count", Value: 200, RawValue: 6},
		},
	}
	res := Evaluate(sd, DefaultThresholds())

	byID := map[int]Penalty{}
	for _, p := range res.Penalties {
		if p.AttributeID != 0 {
			byID[p.AttributeID] = p
		}
	}
	for _, id := range []int{5, 197, 199} {
		if _, ok := byID[id]; !ok {
			t.Errorf("no penalty names attribute %d: %+v", id, res.Penalties)
		}
	}
	if got := byID[5].Points; got <= 0 {
		t.Errorf("attribute 5's penalty is %v points", got)
	}
}

// The temperature check reads attribute 194, so the row it marks is that one — not the drive's
// temperature field, which is not a row in the table.
func TestTheTemperaturePenaltyNamesAttribute194(t *testing.T) {
	hot := 70
	sd := &model.SmartData{
		Bus: model.BusATA, TemperatureC: &hot,
		Attributes: []model.Attribute{{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100}},
	}
	res := Evaluate(sd, DefaultThresholds())

	found := false
	for _, p := range res.Penalties {
		if p.AttributeID == 194 {
			found = true
		}
	}
	if !found {
		t.Errorf("no penalty names attribute 194: %+v", res.Penalties)
	}
}

// An NVMe drive has no ATA attributes at all, so its penalties must not name one — a made-up id
// would mark a row the verdict never looked at.
func TestNVMePenaltiesNameNoAttribute(t *testing.T) {
	sd := &model.SmartData{
		Bus: model.BusNVMe,
		NVMe: &model.NVMeHealth{
			CriticalWarning: 1, AvailableSpare: 4, AvailableSpareThresh: 10, PercentageUsed: 80,
		},
	}
	res := Evaluate(sd, DefaultThresholds())

	if len(res.Penalties) == 0 {
		t.Fatal("expected some penalties for a drive in this state")
	}
	for _, p := range res.Penalties {
		if p.AttributeID != 0 {
			t.Errorf("NVMe penalty %q names attribute %d, which the drive does not have", p.Reason, p.AttributeID)
		}
	}
}

// The lines have to add up to what the score says was taken off. A breakdown that does not sum is
// worse than no breakdown: it is a wrong explanation, which is harder to notice than a missing one.
func TestTheBreakdownAccountsForTheScore(t *testing.T) {
	sd := &model.SmartData{
		Bus: model.BusATA,
		Attributes: []model.Attribute{
			{ID: 5, Name: "Reallocated_Sector_Ct", Value: 90, RawValue: 12},
			{ID: 196, Name: "Reallocated_Event_Count", Value: 90, RawValue: 20},
		},
	}
	res := Evaluate(sd, DefaultThresholds())

	var sum float64
	for _, p := range res.Penalties {
		sum += p.Points
	}
	if sum <= 0 {
		t.Fatalf("penalties sum to %v: %+v", sum, res.Penalties)
	}
	// The score is 100 minus the penalties, floored at the verdict's cap. This drive is Bad, so the
	// cap binds — which is exactly why the sum is checked as "at least what was taken off" rather
	// than "equal to 100 minus the score".
	if want := 100.0 - sum; res.Score > int(want)+1 {
		t.Errorf("score = %d, but the breakdown only accounts for %.1f points off", res.Score, sum)
	}
}
