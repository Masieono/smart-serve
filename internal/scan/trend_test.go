package scan

import (
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
)

func healthy(serial string, score int, rationale string) Device {
	return Device{
		Data:   &model.SmartData{Serial: serial},
		Health: interp.Result{Verdict: model.Good, Score: score, Rationale: rationale},
	}
}

// A drive whose bad sectors are climbing is worse than one sitting at the same count and going
// nowhere, and the score is where that shows (`FEATURES.md` A17).
func TestTrendPenaltiesLowerTheScoreOfAWorseningDrive(t *testing.T) {
	devices := []Device{
		healthy("S1", 74, "5 pending sectors"),
		healthy("S2", 100, "all checks passed"),
	}

	adjusted := applyTrendPenalties(devices, map[string]int64{"S1": 7})

	if adjusted != 1 {
		t.Fatalf("adjusted = %d, want 1", adjusted)
	}
	if devices[0].Health.Score != 69 {
		t.Errorf("S1 score = %d, want 74 less the 5-point penalty", devices[0].Health.Score)
	}
	if devices[1].Health.Score != 100 {
		t.Errorf("S2 score = %d, want it untouched — its count is not moving", devices[1].Health.Score)
	}
	// The rationale says why, as a rate and a window rather than a date.
	for _, want := range []string{"5 points off the score", "7 new sector problems", "30 days"} {
		if !strings.Contains(devices[0].Health.Rationale, want) {
			t.Errorf("rationale = %q, want it to mention %q", devices[0].Health.Rationale, want)
		}
	}
}

// A single new sector is still a finding, and the wording has to survive being singular.
func TestASingleNewSectorIsSingular(t *testing.T) {
	devices := []Device{healthy("S1", 100, "")}
	applyTrendPenalties(devices, map[string]int64{"S1": 1})

	if !strings.Contains(devices[0].Health.Rationale, "1 new sector problem ") {
		t.Errorf("rationale = %q, want the singular", devices[0].Health.Rationale)
	}
	if strings.Contains(devices[0].Health.Rationale, "problems") {
		t.Errorf("rationale = %q, want no plural for one sector", devices[0].Health.Rationale)
	}
}

// An empty rationale is not a reason to render a leading space or a stray separator.
func TestAnEmptyRationaleIsReplacedNotAppendedTo(t *testing.T) {
	devices := []Device{healthy("S1", 100, "")}
	applyTrendPenalties(devices, map[string]int64{"S1": 4})

	if strings.HasPrefix(devices[0].Health.Rationale, " ") {
		t.Errorf("rationale = %q, want no leading space", devices[0].Health.Rationale)
	}
	if !strings.HasPrefix(devices[0].Health.Rationale, "5 points off the score") {
		t.Errorf("rationale = %q", devices[0].Health.Rationale)
	}
}

// A drive nobody just looked at has no score to adjust, and a trend inherited from an earlier scan
// is not evidence about it.
func TestAnUnreadableDriveIsNotPenalised(t *testing.T) {
	devices := []Device{
		{Data: &model.SmartData{Serial: "S1"}, Error: "smartctl: device open failed",
			Health: interp.Result{Verdict: model.Unknown, Score: -1, Rationale: "no S.M.A.R.T. data"}},
	}
	if adjusted := applyTrendPenalties(devices, map[string]int64{"S1": 50}); adjusted != 0 {
		t.Errorf("adjusted = %d, want 0", adjusted)
	}
	if devices[0].Health.Score != -1 {
		t.Errorf("score = %d, want -1 left alone", devices[0].Health.Score)
	}
}

// A drive with no score at all — an unscored verdict — has nothing to take points off.
func TestADriveWithNoScoreIsNotPenalised(t *testing.T) {
	devices := []Device{{Data: &model.SmartData{Serial: "S1"},
		Health: interp.Result{Verdict: model.Unknown, Score: -1}}}
	if adjusted := applyTrendPenalties(devices, map[string]int64{"S1": 50}); adjusted != 0 {
		t.Errorf("adjusted = %d, want 0", adjusted)
	}
}

// A drive absent from the trend map has no history to compare, which is not the same finding as a
// flat one — and neither is a reason to touch its score.
func TestADriveWithNoHistoryIsNotPenalised(t *testing.T) {
	devices := []Device{healthy("S1", 100, "all checks passed")}
	if adjusted := applyTrendPenalties(devices, map[string]int64{}); adjusted != 0 {
		t.Errorf("adjusted = %d, want 0", adjusted)
	}
	if devices[0].Health.Score != 100 {
		t.Errorf("score = %d, want it untouched", devices[0].Health.Score)
	}
}

// A score cannot go negative: the letter grade has no room for it, and a negative score would read
// as a worse drive than one already at zero.
func TestAScoreNeverGoesBelowZero(t *testing.T) {
	devices := []Device{healthy("S1", 2, "nearly gone")}
	applyTrendPenalties(devices, map[string]int64{"S1": 100000})

	if devices[0].Health.Score != 0 {
		t.Errorf("score = %d, want 0 rather than a negative", devices[0].Health.Score)
	}
}
