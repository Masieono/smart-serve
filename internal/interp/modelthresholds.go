package interp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ModelThresholdsSetting is the settings key holding the per-model overrides.
const ModelThresholdsSetting = "model_thresholds"

// ThresholdSettingKeys are the setting names a per-model override may carry, in the order the UI
// shows them.
//
// The *same* names the global settings use, because they tune the same six numbers — one vocabulary
// rather than two. Exported so the API can refuse a name it does not know: a typo'd key would
// otherwise be stored and never read, which is the worst kind of setting, because it looks applied.
var ThresholdSettingKeys = []string{
	"temperature_threshold_c",
	"reallocated_bad_threshold",
	"pending_bad_threshold",
	"uncorrectable_bad_threshold",
	"nvme_percentage_used_warn",
	"warn_on_any_nonzero",
}

// ModelThresholds resolves the interpretation thresholds for one drive: the global settings, with
// any per-model override applied on top (`FEATURES.md` A3).
//
// The global settings answer "how should a drive be judged"; this answers "except for this model".
// It exists because the defaults are a compromise across everything in one box: a consumer SSD and
// an enterprise drive do not have the same idea of a worrying reallocated-sector count, and one
// number cannot be right for both. The alternative was to keep tuning the global numbers until the
// loudest drive was quiet, which is how a monitor ends up blind to the drive that matters.
//
// It does **not** move the report. `A27` requires the exported evidence to name the attributes
// failing the **drive's own manufacturer threshold**, which the drive publishes itself; these
// thresholds only change the verdict this app reaches. The two were already separate paths, and
// keeping them separate is what stops a user's tuning from rewriting an RMA argument.
type ModelThresholds struct {
	base    Thresholds
	entries []modelOverride
}

// modelOverride is one model's partial set of thresholds, keyed by setting name.
//
// A map of the same strings the settings table holds, rather than a struct of pointers: the override
// values are parsed by the same function the global ones are, so a name cannot come to mean two
// things. A pointer-per-field struct would also need a second parser and a second set of rules about
// what "unset" looks like.
type modelOverride struct {
	pattern string
	values  map[string]string
}

// NewModelThresholds builds a resolver from the global thresholds and the stored overrides.
//
// A blank pattern, or an entry that overrides nothing, is dropped rather than kept: an override that
// changes no number is a row in the UI that appears to do something and does not.
func NewModelThresholds(base Thresholds, overrides map[string]map[string]string) ModelThresholds {
	out := ModelThresholds{base: base, entries: make([]modelOverride, 0, len(overrides))}
	for pattern, values := range overrides {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || len(values) == 0 {
			continue
		}
		out.entries = append(out.entries, modelOverride{pattern: pattern, values: values})
	}
	// Longest pattern first, so **the longest matching pattern wins** whatever order the map iterated
	// in — the same tie-break `knowledge.Find` uses when two entries match one drive, and for the
	// same reason: something has to be deterministic, and "the longer name is usually the more
	// precise one" is the best available guess when a user's two patterns both match.
	//
	// It is a tie-break rather than a measure of specificity, and the sharp edge is worth naming: a
	// *broad* pattern that happens to be long (`Samsung SSD`) beats a *narrow* short one (`870 EVO`).
	// The fix is the user's, and it is cheap — name patterns so they do not overlap — which is why
	// the rule is documented rather than worked around.
	sort.SliceStable(out.entries, func(i, j int) bool {
		return len(out.entries[i].pattern) > len(out.entries[j].pattern)
	})
	return out
}

// Base is the global set, for callers with no drive to resolve against — a read that failed before
// the drive's model was known, for instance.
func (m ModelThresholds) Base() Thresholds { return m.base }

// For returns the thresholds that apply to one drive.
//
// The pattern is matched against the model **and** the family, case-insensitively, as a substring —
// the same haystack `knowledge.Find` uses, because smartctl puts the informative string in one or the
// other depending on the drive: a Samsung reports its family, a WD reports only a model with a part
// number in it. A drive that reported neither gets the global set, which is the honest answer rather
// than a guess at which entry was meant.
func (m ModelThresholds) For(model, family string) Thresholds {
	haystack := strings.ToLower(strings.TrimSpace(model + " " + family))
	if haystack == "" {
		return m.base
	}
	for _, entry := range m.entries {
		if !strings.Contains(haystack, strings.ToLower(entry.pattern)) {
			continue
		}
		out := m.base
		for key := range entry.values {
			applyThresholdSetting(&out, entry.values, key)
		}
		return out
	}
	return m.base
}

// ModelThresholdsFromSettings is the one place the `model_thresholds` setting is read.
func ModelThresholdsFromSettings(settings map[string]string) ModelThresholds {
	return NewModelThresholds(FromSettings(settings), ParseModelThresholds(settings[ModelThresholdsSetting]))
}

// ParseModelThresholds decodes the `model_thresholds` setting.
//
// Tolerant on read, strict on write: a value this app cannot parse means "no overrides" rather than
// an error on every scan, and `PUT /api/settings` is where a mistake is reported — at the moment
// somebody made it, naming the field, rather than as a scan that quietly judged drives by defaults.
func ParseModelThresholds(raw string) map[string]map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// ValidateModelThresholds checks a value before it is saved, returning the first problem in words the
// person who typed it can act on.
//
// The keys are checked as well as the shape. A typo'd key is the failure this feature would
// otherwise hide best: the JSON is valid, the save succeeds, the row appears in the editor, and the
// number is never read by anything.
func ValidateModelThresholds(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var parsed map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return fmt.Errorf("must be a JSON object of model name → thresholds: %v", err)
	}
	known := make(map[string]bool, len(ThresholdSettingKeys))
	for _, key := range ThresholdSettingKeys {
		known[key] = true
	}
	for pattern, values := range parsed {
		if strings.TrimSpace(pattern) == "" {
			return errors.New("a model name cannot be blank")
		}
		for key := range values {
			if !known[key] {
				return fmt.Errorf("unknown threshold %q for %q — expected one of: %s",
					key, pattern, strings.Join(ThresholdSettingKeys, ", "))
			}
		}
	}
	return nil
}
