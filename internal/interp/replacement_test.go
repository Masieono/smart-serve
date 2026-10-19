package interp

import (
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/model"
)

type candOpt func(*ReplacementCandidate)

func candidate(serial string, opts ...candOpt) ReplacementCandidate {
	c := ReplacementCandidate{Serial: serial, Health: model.Good, Score: 100}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func withHealth(v model.Verdict, rationale string) candOpt {
	return func(c *ReplacementCandidate) { c.Health, c.Rationale = v, rationale }
}

func withWorsening(n int64) candOpt {
	return func(c *ReplacementCandidate) { c.Worsening = n }
}

// withEndurance is named for what it sets rather than "wear", because `withWear` is already a
// function in this package and the two are not the same thing.
func withEndurance(pct float64) candOpt {
	return func(c *ReplacementCandidate) { c.WearPct = &pct }
}

func withScore(n int) candOpt {
	return func(c *ReplacementCandidate) { c.Score = n }
}

/** The ranking order, by serial. */
func order(ranks []ReplacementRank) []string {
	out := make([]string, 0, len(ranks))
	for _, r := range ranks {
		out = append(out, r.Serial)
	}
	return out
}

func rankOf(ranks []ReplacementRank, serial string) ReplacementRank {
	for _, r := range ranks {
		if r.Serial == serial {
			return r
		}
	}
	return ReplacementRank{}
}

// Every drive is returned, including the healthy ones, so a caller can decide how much of the
// ranking to show. Dropping them here would put that decision in two places.
func TestAHealthyFleetIsAllFine(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{
		candidate("A", withScore(98)),
		candidate("B", withScore(100)),
	})

	if len(ranks) != 2 {
		t.Fatalf("got %d ranks, want 2", len(ranks))
	}
	for _, r := range ranks {
		if r.Band != ReplaceFine {
			t.Errorf("%s band = %q, want fine", r.Serial, r.Band)
		}
		if len(r.Reasons) != 0 {
			t.Errorf("%s reasons = %v, want none", r.Serial, r.Reasons)
		}
	}
	// Within a band, the worst score first.
	if got := order(ranks); got[0] != "A" {
		t.Errorf("order = %v, want the lower score first", got)
	}
}

// The drive's own words are more specific than a band name, so they are used as the reason.
func TestAFailingDriveIsReplaceNowWithItsOwnReason(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{
		candidate("S1", withHealth(model.Bad, "5 reallocated sectors"), withScore(20)),
	})

	r := rankOf(ranks, "S1")
	if r.Band != ReplaceNow {
		t.Errorf("band = %q, want now", r.Band)
	}
	if len(r.Reasons) != 1 || !strings.Contains(r.Reasons[0], "5 reallocated sectors") {
		t.Errorf("reasons = %v, want the drive's own rationale", r.Reasons)
	}
}

// A drive with no rationale still gets a reason: an empty list would make the band unexplained.
func TestABadDriveWithNoRationaleStillHasAReason(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{
		candidate("S1", withHealth(model.Bad, ""), withScore(10)),
	})
	if got := rankOf(ranks, "S1").Reasons; len(got) != 1 || got[0] != "failing now" {
		t.Errorf("reasons = %v, want a fallback", got)
	}
}

func TestACautionDriveIsWatch(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{
		candidate("S1", withHealth(model.Caution, "1 pending sector"), withScore(70)),
	})
	if got := rankOf(ranks, "S1").Band; got != ReplaceWatch {
		t.Errorf("band = %q, want watch", got)
	}
}

// The case the verdict alone cannot express: a drive that is green today and moving is the one that
// surprises you.
func TestAWorseningGreenDriveIsWatchAndSaysWhy(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{candidate("S1", withWorsening(7))})

	r := rankOf(ranks, "S1")
	if r.Band != ReplaceWatch {
		t.Errorf("band = %q, want watch — a moving count is not nothing", r.Band)
	}
	joined := strings.Join(r.Reasons, " ")
	for _, want := range []string{"7 new sector problems", "30 days"} {
		if !strings.Contains(joined, want) {
			t.Errorf("reasons = %v, want them to mention %q", r.Reasons, want)
		}
	}
}

func TestOneNewSectorIsSingular(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{candidate("S1", withWorsening(1))})
	if got := strings.Join(rankOf(ranks, "S1").Reasons, " "); !strings.Contains(got, "1 new sector problem ") {
		t.Errorf("reasons = %v, want the singular", got)
	}
}

// Write endurance is a countdown rather than a state, so it can outrank a healthy verdict: a drive
// with almost no rated writes left is a drive to replace while it is still reporting nothing wrong.
func TestWornOutEnduranceIsReplaceNowWhateverTheVerdictSays(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{candidate("S1", withEndurance(94))})

	r := rankOf(ranks, "S1")
	if r.Band != ReplaceNow {
		t.Errorf("band = %q, want now", r.Band)
	}
	if got := strings.Join(r.Reasons, " "); !strings.Contains(got, "94% of its rated write endurance") {
		t.Errorf("reasons = %v", r.Reasons)
	}
}

func TestEnduranceWellUsedIsReplaceSoon(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{candidate("S1", withEndurance(80))})
	if got := rankOf(ranks, "S1").Band; got != ReplaceSoon {
		t.Errorf("band = %q, want soon", got)
	}
}

// Just under the first threshold is still fine: a band is a judgement, and judgements need edges.
func TestEnduranceBelowTheThresholdIsFine(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{candidate("S1", withEndurance(74.9))})
	if got := rankOf(ranks, "S1").Band; got != ReplaceFine {
		t.Errorf("band = %q, want fine", got)
	}
}

// A failed drive outranks a merely deteriorating one, however fast the second is moving.
func TestAFailureOutranksADeterioration(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{
		candidate("moving", withWorsening(500)),
		candidate("failed", withHealth(model.Bad, "read failure"), withScore(10)),
	})
	if got := order(ranks); got[0] != "failed" {
		t.Errorf("order = %v, want the failure first", got)
	}
}

// A drive with no score is not a drive scoring zero. It sorts last within its band rather than
// first, which is where -1 would otherwise put it.
func TestAnUnscoredDriveSortsLastWithinItsBand(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{
		candidate("unscored", withHealth(model.Caution, "no score"), withScore(-1)),
		candidate("scored", withHealth(model.Caution, "a low score"), withScore(40)),
	})
	if got := order(ranks); got[0] != "scored" {
		t.Errorf("order = %v, want the scored drive first", got)
	}
}

// Ties break on serial so the ranking does not shuffle between renders.
func TestTheOrderIsStableForIdenticalDrives(t *testing.T) {
	first := order(RankForReplacement([]ReplacementCandidate{
		candidate("c", withScore(50)),
		candidate("a", withScore(50)),
		candidate("b", withScore(50)),
	}))
	second := order(RankForReplacement([]ReplacementCandidate{
		candidate("b", withScore(50)),
		candidate("c", withScore(50)),
		candidate("a", withScore(50)),
	}))
	if strings.Join(first, ",") != strings.Join(second, ",") {
		t.Errorf("order changed with the input: %v then %v", first, second)
	}
}

// Several reasons accumulate rather than replacing each other: a drive can be caution *and* moving
// *and* worn, and the reader should see all three.
func TestReasonsAccumulate(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{
		candidate("S1", withHealth(model.Caution, "1 pending sector"), withWorsening(3), withEndurance(80)),
	})
	if got := len(rankOf(ranks, "S1").Reasons); got != 3 {
		t.Errorf("got %d reasons, want 3", got)
	}
}

// A drive the app could not score is still ranked: "we do not know" is a reason to look, not a
// reason to hide it.
func TestAnUnscoredDriveIsStillRanked(t *testing.T) {
	ranks := RankForReplacement([]ReplacementCandidate{
		candidate("S1", withHealth(model.Unknown, "no S.M.A.R.T. data"), withScore(-1)),
	})
	if len(ranks) != 1 {
		t.Fatalf("got %d ranks, want 1", len(ranks))
	}
}

func TestAnEmptyFleetIsNotAnError(t *testing.T) {
	if got := RankForReplacement(nil); len(got) != 0 {
		t.Errorf("RankForReplacement(nil) = %v, want empty", got)
	}
}
