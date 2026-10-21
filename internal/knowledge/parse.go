package knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// FormatVersion is the only document version this build reads.
//
// Checked on load rather than assumed, so a file written for a later version is refused instead of
// half-understood — an entry whose meaning changed is worse than a missing one.
const FormatVersion = 1

// minFamilyLength is the shortest family string that can match.
//
// Substring matching is what makes the table work against real model strings, and it is also what
// could make it *wrong*: a two-letter family would match most of a fleet. Five is short enough for
// "MX500" and long enough that an accidental match needs a real coincidence.
const minFamilyLength = 5

// Parse reads a knowledge document, refusing anything it cannot use.
//
// Strict rather than tolerant, and the source is why: this file arrives from a URL or a text box, and
// an entry silently dropped for a typo looks *exactly* like the app not knowing a drive — which is
// the one failure a knowledge base cannot afford, because it is indistinguishable from having no data
// at all. Unknown fields are refused for the same reason: a misspelled key is a typo, and ignoring it
// produces an entry that looks fine and says less than its author meant.
func Parse(data []byte) (Base, error) {
	var b Base
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return Base{}, fmt.Errorf("not a knowledge document: %w", err)
	}
	if err := b.Validate(); err != nil {
		return Base{}, err
	}
	return b, nil
}

// Validate reports the first thing wrong with a document, in the terms a person editing it needs.
func (b Base) Validate() error {
	if b.Version != FormatVersion {
		return fmt.Errorf("unsupported format version %d; this build reads version %d",
			b.Version, FormatVersion)
	}
	if len(b.Drives) == 0 {
		return errors.New("no drives: a knowledge base with no entries would replace nothing with nothing")
	}

	seen := map[string]bool{}
	for i, e := range b.Drives {
		where := fmt.Sprintf("drives[%d]", i)
		family := strings.TrimSpace(e.Family)
		switch {
		case family == "":
			return fmt.Errorf("%s: family is required", where)
		case len(family) < minFamilyLength:
			return fmt.Errorf("%s (%s): family is shorter than %d characters, so it would match "+
				"unrelated drives", where, family, minFamilyLength)
		case seen[strings.ToLower(family)]:
			return fmt.Errorf("%s: %s appears twice; one entry per family", where, family)
		}
		seen[strings.ToLower(family)] = true

		// Every pattern is length-checked, not just the name: `matches` exists precisely so a short
		// *suffix* can be written down, and a pattern short enough to match unrelated drives is the
		// failure mode this whole scheme has to avoid.
		for _, pattern := range e.patterns() {
			if len(strings.TrimSpace(pattern)) < minFamilyLength {
				return fmt.Errorf("%s (%s): %q is shorter than %d characters, so it would match "+
					"unrelated drives", where, family, pattern, minFamilyLength)
			}
		}

		if e.RatedTBW == nil && len(e.RatedTBWByCapacityGB) == 0 &&
			e.RatedWorkloadTBPerYear == nil && len(e.Advisories) == 0 {
			return fmt.Errorf("%s (%s): nothing to say — add a rating or an advisory", where, family)
		}
		if e.RatedTBW != nil && *e.RatedTBW <= 0 {
			return fmt.Errorf("%s (%s): rated_tbw must be positive", where, family)
		}
		if e.RatedWorkloadTBPerYear != nil && *e.RatedWorkloadTBPerYear <= 0 {
			return fmt.Errorf("%s (%s): rated_workload_tb_per_year must be positive", where, family)
		}
		for key, v := range e.RatedTBWByCapacityGB {
			if _, err := strconv.ParseFloat(key, 64); err != nil {
				return fmt.Errorf("%s (%s): %q is not a capacity in GB", where, family, key)
			}
			if v <= 0 {
				return fmt.Errorf("%s (%s): rated_tbw_by_capacity_gb[%s] must be positive",
					where, family, key)
			}
		}

		for j, a := range e.Advisories {
			at := fmt.Sprintf("%s.advisories[%d]", where, j)
			if strings.TrimSpace(a.Title) == "" {
				return fmt.Errorf("%s: title is required", at)
			}
			switch a.Severity {
			case SeverityInfo, SeverityWarning, SeverityCritical:
			default:
				return fmt.Errorf("%s: severity must be info, warning or critical (got %q)",
					at, a.Severity)
			}
		}
	}
	return nil
}
