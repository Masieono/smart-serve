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
