package interp

import (
	"strings"
	"testing"
)

// The overrides are the point of the feature, so the rule for which one applies is pinned here.
//
// Every case is a way the feature could be quietly wrong: an override that never applies, one that
// applies to the wrong drive, or one that silently discards the global settings it was supposed to
// build on.
func TestModelThresholdsFor(t *testing.T) {
	base := DefaultThresholds()
	base.ReallocatedBad = 10
	base.TemperatureC = 55

	resolver := NewModelThresholds(base, map[string]map[string]string{
		"870 EVO":      {"reallocated_bad_threshold": "1"},
		"990 PRO":      {"temperature_threshold_c": "70"},
		"WD40EFRX":     {"reallocated_bad_threshold": "5", "warn_on_any_nonzero": "false"},
		"  spaced  ":   {"pending_bad_threshold": "3"},
		"ignored":      {},
		"":             {"pending_bad_threshold": "9"},
		"nonsense key": {"not_a_setting": "1"},
	})

	cases := []struct {
		name        string
		model       string
		family      string
		reallocated int
		temperature int
		pending     int
		anyNonZero  bool
	}{
		// The global values, untouched.
		{"an unlisted model", "TOSHIBA DT01ACA200", "", 10, 55, 10, true},
		// A drive with no model string and no family gets the globals rather than a guess.
		{"no model at all", "", "", 10, 55, 10, true},

		{"a model override", "Samsung SSD 870 EVO 500GB", "", 1, 55, 10, true},
		// Matching is against the model *and* the family, because smartctl puts the informative
		// string in one or the other depending on the drive: this drive reports the family and no
		// model at all, which is what a Samsung SSD looks like from smartctl.
		{"matched on the family", "Samsung SSD", "870 EVO", 1, 55, 10, true},
		// Case-insensitive, because a model string is a label rather than a key.
		{"case-insensitively", "samsung ssd 870 evo", "", 1, 55, 10, true},
		{"a different model, a different override", "Samsung SSD 990 PRO 2TB", "", 10, 70, 10, true},

		// Only the named fields change; the rest come from the globals.
		{"a single field", "WDC WD40EFRX-68N32N0", "", 5, 55, 10, false},

		{"a pattern with padding still matches", "spaced out drive", "", 10, 55, 3, true},
		// An override with no values is not an entry, and a blank pattern is not a model.
		{"an empty override is ignored", "ignored model", "", 10, 55, 10, true},
		{"a blank pattern is ignored", "", "", 10, 55, 10, true},
		// A key that names no setting changes nothing rather than zeroing something.
		{"an unknown key changes nothing", "nonsense key model", "", 10, 55, 10, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolver.For(tc.model, tc.family)
			if got.ReallocatedBad != tc.reallocated {
				t.Errorf("ReallocatedBad = %d, want %d", got.ReallocatedBad, tc.reallocated)
			}
			if got.TemperatureC != tc.temperature {
				t.Errorf("TemperatureC = %d, want %d", got.TemperatureC, tc.temperature)
			}
			if got.PendingBad != tc.pending {
				t.Errorf("PendingBad = %d, want %d", got.PendingBad, tc.pending)
			}
			if got.WarnOnAnyNonZero != tc.anyNonZero {
				t.Errorf("WarnOnAnyNonZero = %v, want %v", got.WarnOnAnyNonZero, tc.anyNonZero)
			}
		})
	}
}

// The tie-break when two patterns both match: the longer one wins, whatever order the map iterated
// in. The same rule `knowledge.Find` uses, and pinned here because the failure it prevents — a
// different entry applying on alternate runs — is invisible in a running app.
func TestTheLongestMatchingPatternWins(t *testing.T) {
	base := DefaultThresholds()
	base.ReallocatedBad = 10
	resolver := NewModelThresholds(base, map[string]map[string]string{
		"Samsung":              {"reallocated_bad_threshold": "8"},
		"Samsung SSD 870 EVO":  {"reallocated_bad_threshold": "1"},
		"WDC WD40EFRX-68N32N0": {"reallocated_bad_threshold": "5"},
	})

	if got := resolver.For("Samsung SSD 870 EVO", "").ReallocatedBad; got != 1 {
		t.Errorf("ReallocatedBad = %d, want 1 — the longest pattern should win", got)
	}
	if got := resolver.For("Samsung SSD 860", "").ReallocatedBad; got != 8 {
		t.Errorf("ReallocatedBad = %d, want 8 for a drive matching only the short entry", got)
	}
	// A pattern with no overlap at all is matched on its own merits.
	if got := resolver.For("WDC WD40EFRX-68N32N0", "").ReallocatedBad; got != 5 {
		t.Errorf("ReallocatedBad = %d, want 5", got)
	}
}

// The sharp edge of that tie-break, pinned so it is a known property rather than a surprise: a
// *broad* pattern that happens to be long beats a *narrow* short one. The rule is a tie-break, not a
// measure of specificity, and the remedy belongs to the user — name patterns so they do not overlap.
func TestALongBroadPatternBeatsAShortNarrowOne(t *testing.T) {
	base := DefaultThresholds()
	base.ReallocatedBad = 10
	resolver := NewModelThresholds(base, map[string]map[string]string{
		"Samsung SSD": {"reallocated_bad_threshold": "8"},
		"870 EVO":     {"reallocated_bad_threshold": "1"},
	})

	if got := resolver.For("Samsung SSD 870 EVO", "").ReallocatedBad; got != 8 {
		t.Errorf("ReallocatedBad = %d, want 8 — the longer pattern wins even when it is the broader one", got)
	}
}

// `0` is not expressible as a threshold: the shared parser reads a positive integer, so an override
// of `0` leaves the global value in place. A deliberate consequence of reusing the settings parser,
// pinned so nobody "fixes" it into a drive that can never fail.
func TestAModelOverrideOfZeroKeepsTheGlobalValue(t *testing.T) {
	base := DefaultThresholds()
	base.ReallocatedBad = 10
	got := NewModelThresholds(base, map[string]map[string]string{
		"tank": {"reallocated_bad_threshold": "0"},
	}).For("tank drive", "")

	if got.ReallocatedBad != 10 {
		t.Errorf("ReallocatedBad = %d, want the global 10 — 0 is not a threshold", got.ReallocatedBad)
	}
}

// A resolver built from the settings map is what the scan actually uses, so the two halves — reading
// the global numbers and reading the overrides — are checked together.
func TestModelThresholdsFromSettings(t *testing.T) {
	settings := map[string]string{
		"reallocated_bad_threshold": "25",
		ModelThresholdsSetting:      `{"870 EVO":{"reallocated_bad_threshold":"2"}}`,
	}
	resolver := ModelThresholdsFromSettings(settings)

	if got := resolver.For("Samsung SSD 870 EVO", "").ReallocatedBad; got != 2 {
		t.Errorf("override = %d, want 2", got)
	}
	if got := resolver.For("Samsung SSD 870 EVO", "").PendingBad; got != DefaultThresholds().PendingBad {
		t.Errorf("PendingBad = %d, want the shipped default", got)
	}
	if got := resolver.For("Other drive", "").ReallocatedBad; got != 25 {
		t.Errorf("global = %d, want the tuned 25", got)
	}
}

// An unreadable value means "no overrides" rather than an error on every scan: the point of the
// feature is to judge drives, and refusing to scan because a setting is malformed would be worse than
// the malformed setting. `PUT /api/settings` is where it is refused, at the point it was typed.
func TestAnUnreadableOverrideSettingMeansNoOverrides(t *testing.T) {
	for _, raw := range []string{"", "   ", "not json", "[]", `{"model":"not an object"}`} {
		if got := ParseModelThresholds(raw); len(got) != 0 {
			t.Errorf("ParseModelThresholds(%q) = %v, want nothing", raw, got)
		}
	}
}

func TestValidateModelThresholds(t *testing.T) {
	good := []string{
		"",
		"   ",
		"{}",
		`{"870 EVO":{"reallocated_bad_threshold":"1"}}`,
		`{"870 EVO":{"warn_on_any_nonzero":"false"}}`,
		`{"a":{"temperature_threshold_c":"70"},"b":{"pending_bad_threshold":"3"}}`,
	}
	for _, raw := range good {
		if err := ValidateModelThresholds(raw); err != nil {
			t.Errorf("ValidateModelThresholds(%q) = %v, want no error", raw, err)
		}
	}

	bad := []struct {
		raw  string
		want string
	}{
		{"not json", "JSON"},
		{`[]`, "JSON"},
		{`{"870 EVO":"1"}`, "JSON"},
		// A typo'd key is the failure this feature would otherwise hide best: the JSON is valid, the
		// save succeeds, the row appears, and the number is never read.
		{`{"870 EVO":{"reallocated_bad":"1"}}`, "unknown threshold"},
		{`{"":{"reallocated_bad_threshold":"1"}}`, "blank"},
	}
	for _, tc := range bad {
		err := ValidateModelThresholds(tc.raw)
		if err == nil {
			t.Errorf("ValidateModelThresholds(%q) = nil, want an error", tc.raw)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateModelThresholds(%q) = %q, want it to mention %q", tc.raw, err, tc.want)
		}
	}
}

// The list the API validates against has to be the list the parser understands, or the two would
// disagree about which keys are real — silently, in both directions.
func TestEveryThresholdSettingKeyIsApplied(t *testing.T) {
	for _, key := range ThresholdSettingKeys {
		// A value that differs from the shipped default, so "applied" is observable.
		value := "7"
		if key == "warn_on_any_nonzero" {
			value = "false"
		}
		before := DefaultThresholds()
		after := before
		applyThresholdSetting(&after, map[string]string{key: value}, key)
		if after == before {
			t.Errorf("setting %q changed nothing — the API accepts a name nothing reads", key)
		}
	}
}
