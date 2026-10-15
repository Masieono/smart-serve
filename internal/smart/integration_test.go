package smart_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/smart"
)

// TestFixtureVerdicts is the end-to-end interpretation check: a realistic
// smartctl document in, a verdict and score out (docs/SMART_INTERPRETATION.md).
func TestFixtureVerdicts(t *testing.T) {
	cases := []struct {
		file    string
		verdict model.Verdict
		score   int
	}{
		{"ata-healthy.json", model.Good, 100},
		{"ata-pending.json", model.Caution, 74},
		{"ata-failed.json", model.Bad, 10},
		{"ata-packed.json", model.Caution, 74},
		{"nvme-healthy.json", model.Good, 100},
		{"nvme-worn.json", model.Caution, 65},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			sd, err := smart.ParseSMART(data)
			if err != nil {
				t.Fatalf("ParseSMART: %v", err)
			}
			got := interp.Evaluate(sd, interp.DefaultThresholds())
			if got.Verdict != tc.verdict {
				t.Errorf("verdict = %q, want %q (%s)", got.Verdict, tc.verdict, got.Rationale)
			}
			if got.Score != tc.score {
				t.Errorf("score = %d, want %d (%s)", got.Score, tc.score, got.Rationale)
			}
		})
	}
}

// TestSamsungWearFromAttribute177 is the fixture for a drive that reports wear as attribute
// 177 and nothing else — no 231, which was the only id the app read.
//
// It is kept because the rule that makes it work is a vendor gate, and a gate is exactly
// the kind of thing that quietly stops matching. This drive's real attribute table is what
// caught the first version of the rule reading `life_left_pct: null` on the API path while
// the stored value was a correct 5% used.
func TestSamsungWearFromAttribute177(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ata-samsung-850-evo.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	sd, err := smart.ParseSMART(data)
	if err != nil {
		t.Fatalf("ParseSMART: %v", err)
	}
	if sd.ModelFamily != "Samsung based SSDs" {
		t.Fatalf("model family = %q, want the fixture to carry it", sd.ModelFamily)
	}

	left, ok := interp.ATALifeLeftPct(sd)
	if !ok || left != 95 {
		t.Errorf("life left = (%d, %v), want (95, true) from attribute 177", left, ok)
	}

	m := interp.DeriveMetrics(sd)
	if m.LifeLeftPct == nil || *m.LifeLeftPct != 95 {
		t.Errorf("metrics life left = %v, want 95", m.LifeLeftPct)
	}
	if m.PowerOnHours == nil || *m.PowerOnHours != 26193 {
		t.Errorf("power-on hours = %v, want 26193", m.PowerOnHours)
	}
	// 2,575,292,682 LBAs of 512 bytes is the 13.2 TB the app showed for this drive.
	if m.DataWrittenBytes == nil || *m.DataWrittenBytes != 2575292682*512 {
		t.Errorf("data written = %v, want %d", m.DataWrittenBytes, int64(2575292682)*512)
	}
}
