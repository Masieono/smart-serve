package interp

import (
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/model"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func atDay(days float64) time.Time {
	return epoch.Add(time.Duration(days * 24 * float64(time.Hour)))
}

func wear(v float64) *float64 { return &v }
func sectors(v int64) *int64  { return &v }

// No history, or history too short to carry a rate, must say so rather than inventing a
// horizon from one reading.
func TestProjectNeedsHistoryBeforeItWillGuess(t *testing.T) {
	cases := map[string][]Observation{
		"nothing at all": nil,
		"one reading":    {{TS: atDay(0), WearPct: wear(5), Sectors: sectors(0)}},
		// Two readings an hour apart: the wear counter has not had room to move for any
		// reason other than rounding.
		"too short a span": {
			{TS: atDay(0), WearPct: wear(5), Sectors: sectors(0)},
			{TS: atDay(0.04), WearPct: wear(5), Sectors: sectors(0)},
		},
	}
	for name, obs := range cases {
		got := Project(obs)
		if got.Basis != "none" {
			t.Errorf("%s: basis = %q, want none", name, got.Basis)
		}
		if got.Worsening != "" {
			t.Errorf("%s: worsening = %q, want empty", name, got.Worsening)
		}
		if !strings.Contains(got.Summary, "Not enough history") {
			t.Errorf("%s: summary = %q", name, got.Summary)
		}
	}
}

// A drive that wears at a steady rate gets a horizon, and a confidence that reflects how
// long it was watched.
func TestProjectExtrapolatesWear(t *testing.T) {
	got := Project([]Observation{
		{TS: atDay(0), WearPct: wear(5)},
		{TS: atDay(30), WearPct: wear(20)},
	})

	if got.Basis != "wear" {
		t.Fatalf("basis = %q, want wear", got.Basis)
	}
	// 15 points over 30 days is 0.5/day, so 80% remaining is 160 days.
	if !strings.Contains(got.Summary, "80%") || !strings.Contains(got.Summary, "160 days") {
		t.Errorf("summary = %q, want 80%% and 160 days", got.Summary)
	}
	// A 30-day window is the point at which the rate is worth believing at all.
	if got.Confidence != "medium" {
		t.Errorf("confidence = %q, want medium for a 30-day window", got.Confidence)
	}
	if got.Caveat == "" {
		t.Error("a projection must carry its caveat")
	}
}

func TestProjectLowersConfidenceOnAShortWindow(t *testing.T) {
	got := Project([]Observation{
		{TS: atDay(0), WearPct: wear(5)},
		{TS: atDay(10), WearPct: wear(20)},
	})
	if got.Confidence != "low" {
		t.Errorf("confidence = %q, want low for a 10-day window", got.Confidence)
	}
	if !strings.Contains(got.Caveat, "two readings") {
		t.Errorf("caveat = %q, want it to name the weakness", got.Caveat)
	}
}

// A wear counter that has not moved is a real, reportable finding — and a change inside
// the counter's own rounding is not a trend.
func TestProjectTreatsFlatWearAsFlat(t *testing.T) {
	for name, delta := range map[string]float64{"unchanged": 0, "inside rounding": 0.2} {
		got := Project([]Observation{
			{TS: atDay(0), WearPct: wear(7)},
			{TS: atDay(90), WearPct: wear(7 + delta)},
		})
		if got.Basis != "wear" {
			t.Errorf("%s: basis = %q, want wear", name, got.Basis)
		}
		if !strings.Contains(got.Summary, "not measurably increased") {
			t.Errorf("%s: summary = %q", name, got.Summary)
		}
	}
}

// Sector growth is reported as a direction, never as a date. A rate taken through two
// bursts looks precise and means nothing.
func TestProjectReportsSectorGrowthWithoutADate(t *testing.T) {
	got := Project([]Observation{
		{TS: atDay(0), Sectors: sectors(0)},
		{TS: atDay(30), Sectors: sectors(5)},
	})

	if got.Basis != "sectors" {
		t.Fatalf("basis = %q, want sectors", got.Basis)
	}
	if got.Summary != "5 new sector problems in 30 days" {
		t.Errorf("summary = %q", got.Summary)
	}
	if got.Confidence != "low" {
		t.Errorf("confidence = %q, want low", got.Confidence)
	}
	if !strings.Contains(got.Caveat, "bursts") {
		t.Errorf("caveat = %q, want it to explain why there is no date", got.Caveat)
	}
	if got.Worsening != got.Summary {
		t.Errorf("worsening = %q, want it to match the summary %q", got.Worsening, got.Summary)
	}
}

func TestProjectTreatsAStableSectorCountAsGoodNews(t *testing.T) {
	got := Project([]Observation{
		{TS: atDay(0), Sectors: sectors(3)},
		{TS: atDay(60), Sectors: sectors(3)},
	})
	if !strings.Contains(got.Summary, "No new sector problems in 60 days") {
		t.Errorf("summary = %q", got.Summary)
	}
	if got.Worsening != "" {
		t.Errorf("worsening = %q, want empty for a stable count", got.Worsening)
	}
}

// A drive can be wearing normally *and* throwing new bad sectors, so the two are computed
// independently: the summary prefers wear, but the warning still fires.
func TestProjectFlagsWorseningEvenWhenWearLeads(t *testing.T) {
	got := Project([]Observation{
		{TS: atDay(0), WearPct: wear(5), Sectors: sectors(0)},
		{TS: atDay(30), WearPct: wear(20), Sectors: sectors(9)},
	})

	if got.Basis != "wear" {
		t.Fatalf("basis = %q, want wear to take precedence", got.Basis)
	}
	if got.Worsening != "9 new sector problems in 30 days" {
		t.Errorf("worsening = %q, want the sector growth", got.Worsening)
	}
}

// The badge has to clear itself, or it is not a warning. A drive that gained sectors months
// ago and has been stable since is not getting worse, however much worse it is than it was.
func TestWorseningDecaysOnceGrowthStops(t *testing.T) {
	stopped := Project([]Observation{
		{TS: atDay(0), Sectors: sectors(0)},
		{TS: atDay(40), Sectors: sectors(7)},
		{TS: atDay(80), Sectors: sectors(7)},
	})
	if stopped.Worsening != "" {
		t.Errorf("worsening = %q, want empty once growth has stopped", stopped.Worsening)
	}

	// Still gaining: the comparison is against the recent reading, not the oldest one, so
	// the message reports only the growth that is actually recent.
	growing := Project([]Observation{
		{TS: atDay(0), Sectors: sectors(0)},
		{TS: atDay(40), Sectors: sectors(7)},
		{TS: atDay(80), Sectors: sectors(9)},
	})
	if growing.Worsening != "2 new sector problems in 40 days" {
		t.Errorf("worsening = %q, want the growth since day 40", growing.Worsening)
	}
}

// A short history must not hide a drive that is already shedding sectors: with less than
// the comparison window recorded, the fallback is the oldest reading. Otherwise the feature
// would be silent for its first month, which is exactly when a new install is watched most
// closely.
func TestWorseningFallsBackWhenHistoryIsShort(t *testing.T) {
	got := Project([]Observation{
		{TS: atDay(0), Sectors: sectors(0)},
		{TS: atDay(5), Sectors: sectors(4)},
	})
	if got.Worsening != "4 new sector problems in 5 days" {
		t.Errorf("worsening = %q, want the whole-history fallback", got.Worsening)
	}
}

// A drive that reports neither must not be told it is healthy.
func TestProjectWithoutWearOrSectors(t *testing.T) {
	got := Project([]Observation{
		{TS: atDay(0), Hours: sectors(100)},
		{TS: atDay(30), Hours: sectors(800)},
	})
	if got.Basis != "none" {
		t.Errorf("basis = %q, want none", got.Basis)
	}
	if got.Worsening != "" {
		t.Errorf("worsening = %q, want empty", got.Worsening)
	}
}

func TestSectorIssuesSumsTheThreeCounts(t *testing.T) {
	attrs := []model.Attribute{
		{ID: 5, RawValue: 3},
		{ID: 197, RawValue: 2},
		{ID: 198, RawValue: 1},
		{ID: 9, RawValue: 12345}, // power-on hours, not a sector count
	}
	if got := SectorIssues(attrs); got != 6 {
		t.Errorf("SectorIssues = %d, want 6", got)
	}
	if got := SectorIssues(nil); got != 0 {
		t.Errorf("SectorIssues(nil) = %d, want 0", got)
	}
}

// The packed-word rule applies here too: some vendors pack several 16-bit counters into
// one raw value, and reading it whole would report a wildly wrong sector count.
func TestSectorIssuesUnpacksPackedWords(t *testing.T) {
	attrs := []model.Attribute{{ID: 5, RawValue: 5 << 16}}
	if got := SectorIssues(attrs); got != 5 {
		t.Errorf("SectorIssues = %d, want 5 after unpacking", got)
	}
}

func TestHumanDaysScalesToTheMagnitude(t *testing.T) {
	cases := map[float64]string{
		0.5:  "less than a day",
		12:   "12 days",
		59:   "59 days",
		160:  "160 days",
		320:  "320 days",
		364:  "364 days",
		400:  "1.1 years",
		900:  "2.5 years",
		3650: "10 years",
	}
	for days, want := range cases {
		if got := humanDays(days); got != want {
			t.Errorf("humanDays(%v) = %q, want %q", days, got, want)
		}
	}
}

// A17. The penalty has to be bounded and explainable: a trend is an extrapolation from a handful
// of readings, and a score is the number people quote and compare.
func TestTrendPenaltyScalesByDoublingAndStops(t *testing.T) {
	cases := []struct {
		added int64
		want  int
	}{
		{0, 0}, {-5, 0},
		{1, 3},
		{2, 4}, {3, 4},
		{4, 5}, {7, 5},
		{8, 6}, {15, 6},
		{16, 7}, {31, 7},
		{32, 8}, {63, 8},
		{64, 9}, {127, 9},
		{128, TrendPenaltyCap},
		{1_000_000, TrendPenaltyCap},
	}
	for _, c := range cases {
		if got := TrendPenalty(c.added); got != c.want {
			t.Errorf("TrendPenalty(%d) = %d, want %d", c.added, got, c.want)
		}
	}
}

// The cap exists so the trend can never carry a drive out of its verdict on its own.
func TestTrendPenaltyNeverExceedsTheCap(t *testing.T) {
	for added := int64(1); added < 100000; added++ {
		if got := TrendPenalty(added); got > TrendPenaltyCap {
			t.Fatalf("TrendPenalty(%d) = %d, above the cap of %d", added, got, TrendPenaltyCap)
		}
	}
}

// More new sectors is never *better* than fewer. A rule that could fall as growth rose would make
// the score jump around for no reason a reader could follow.
func TestTrendPenaltyNeverFallsAsGrowthRises(t *testing.T) {
	previous := 0
	for added := int64(1); added < 5000; added++ {
		got := TrendPenalty(added)
		if got < previous {
			t.Fatalf("TrendPenalty(%d) = %d, below the previous %d", added, got, previous)
		}
		previous = got
	}
}

// Any growth costs something: a drive that has reallocated even one sector has lost storage it
// cannot get back, and the count going *up* is the difference between a drive to watch and a drive
// to watch closely.
func TestAnyGrowthCostsSomething(t *testing.T) {
	if TrendPenalty(1) == 0 {
		t.Error("TrendPenalty(1) = 0, but one reallocated sector is a finding")
	}
}
