package knowledge

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// capacityTolerance is how far a drive's capacity may be from a table bucket and still use its figure.
//
// Vendors sell "500 GB" drives that report 500.1 GB and "512 GB" drives that report 512.1, and the
// decimal-versus-binary mismatch adds a few percent either way. Ten percent covers all of that and
// still refuses to quote a 4 TB rating for a 3 TB drive.
const capacityTolerance = 0.10

// approxBelow is the distance under which a capacity bucket counts as exact, so the app can say
// "600 TBW" rather than "about 600 TBW".
const approxBelow = 0.02

// Merge layers knowledge documents: the bundled table first, then whatever was fetched, then the
// user's overrides. Later layers win.
//
// An entry **replaces** the one for the same family rather than merging field by field. Field-level
// merging would be cleverer and worse: it makes it impossible to *remove* an advisory, and "I deleted
// it and it is still there" is a bug report nobody can act on. The overrides editor seeds itself with
// the resolved entry, so the common edit — change one number — keeps everything else without anyone
// having to think about precedence.
//
// Layers are assumed to have been through `Parse`; this does not validate.
func Merge(layers ...Base) Base {
	out := Base{Version: FormatVersion}
	index := map[string]int{} // lower-cased family -> position in out.Drives

	for _, layer := range layers {
		for _, e := range layer.Drives {
			key := strings.ToLower(strings.TrimSpace(e.Family))
			if at, ok := index[key]; ok {
				out.Drives[at] = e
				continue
			}
			index[key] = len(out.Drives)
			out.Drives = append(out.Drives, e)
		}
		if layer.Updated != "" {
			out.Updated = layer.Updated
		}
		if layer.Note != "" {
			out.Note = layer.Note
		}
	}

	// Sorted, so the same set of layers always produces the same document — which matters because the
	// overrides editor hands the user a copy of it to edit.
	sort.SliceStable(out.Drives, func(i, j int) bool { return out.Drives[i].Family < out.Drives[j].Family })
	return out
}

// Find matches a drive against the base.
//
// The match is on the model **and** the model family, because either can be the informative one:
// smartctl reports "Samsung SSD 870 EVO" as a family and "WDC WD40EFRX-68N32N0" as a model with no
// family at all. The **longest** matching family wins, so a specific entry beats a general one
// whatever order they appear in the file — and so a user's more precise override wins for the same
// reason rather than by accident of position.
func (b Base) Find(model, family, firmware string, capacityBytes *uint64) Lookup {
	haystack := strings.ToLower(strings.TrimSpace(model + " " + family))

	var best *Entry
	for i := range b.Drives {
		e := &b.Drives[i]
		if !e.matches(haystack) {
			continue
		}
		// The longest pattern wins, so a specific entry beats a general one whatever order they
		// appear in the file — and so a user's more precise override wins for the same reason rather
		// than by accident of position.
		if best == nil || e.specificity() > best.specificity() {
			best = e
		}
	}
	if best == nil {
		return Lookup{}
	}

	out := Lookup{
		Entry: best, Family: best.Family,
		RatedWorkloadTBPerYear: best.RatedWorkloadTBPerYear,
		Source:                 best.Source, Notes: best.Notes,
	}
	out.RatedTBW, out.RatedTBWApprox = best.tbwFor(capacityBytes)
	out.Advisories = best.advisoriesFor(firmware)
	return out
}

// patterns are the strings this entry matches on: `matches` when it has any, `family` otherwise.
func (e *Entry) patterns() []string {
	if len(e.Matches) > 0 {
		return e.Matches
	}
	return []string{e.Family}
}

// matches reports whether any of the entry's patterns occurs in the lower-cased haystack.
//
// The length floor is what keeps substring matching from matching everything: a two-character pattern
// would claim most of a fleet, and a wrong entry is worse than a missing one because it carries a
// number that looks authoritative.
func (e *Entry) matches(haystack string) bool {
	for _, pattern := range e.patterns() {
		needle := strings.ToLower(strings.TrimSpace(pattern))
		if len(needle) >= minFamilyLength && strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// specificity is how precise an entry is, for "the longest pattern wins". An entry that lists several
// patterns is scored on its shortest, because that is the one that could match loosely.
func (e *Entry) specificity() int {
	shortest := 0
	for _, pattern := range e.patterns() {
		if n := len(strings.TrimSpace(pattern)); shortest == 0 || n < shortest {
			shortest = n
		}
	}
	return shortest
}

// tbwFor resolves the endurance for one drive's capacity.
func (e *Entry) tbwFor(capacityBytes *uint64) (*float64, bool) {
	if len(e.RatedTBWByCapacityGB) == 0 || capacityBytes == nil {
		// A flat figure, if there is one. A family whose per-capacity ratings are not published yet
		// still gets its headline number quoted, and the app says where it came from.
		return e.RatedTBW, false
	}

	gb := float64(*capacityBytes) / 1e9
	bestKey, bestDist := "", math.Inf(1)
	for key := range e.RatedTBWByCapacityGB {
		bucket, err := strconv.ParseFloat(key, 64)
		if err != nil || bucket <= 0 {
			continue
		}
		if dist := math.Abs(gb-bucket) / bucket; dist < bestDist {
			bestKey, bestDist = key, dist
		}
	}
	if bestKey == "" || bestDist > capacityTolerance {
		// Nothing close enough. Saying nothing is right: a rating for a different capacity is a
		// number about a different drive, and the one thing this table must not do is invent one.
		return nil, false
	}
	v := e.RatedTBWByCapacityGB[bestKey]
	return &v, bestDist > approxBelow
}

// advisoriesFor picks the advisories that apply to one drive's firmware.
//
// `Firmware` may name several versions, comma-separated — "SVT01B6Q, SVT02B6Q" — because a fix
// usually covers everything before it, and listing them is clearer than inventing a range syntax.
func (e *Entry) advisoriesFor(firmware string) []Advisory {
	have := strings.TrimSpace(firmware)
	var out []Advisory
	for _, a := range e.Advisories {
		named := strings.TrimSpace(a.Firmware)
		if named == "" {
			out = append(out, a)
			continue
		}
		// A firmware-specific advisory cannot match a drive that reported none: we would be guessing
		// at the one fact the advisory is about.
		if have == "" {
			continue
		}
		for _, want := range strings.Split(named, ",") {
			if strings.EqualFold(strings.TrimSpace(want), have) {
				out = append(out, a)
				break
			}
		}
	}
	// Worst first, so a critical advisory is the one that gets read.
	sort.SliceStable(out, func(i, j int) bool {
		return severityRank(out[i].Severity) > severityRank(out[j].Severity)
	})
	return out
}

func severityRank(severity string) int {
	switch severity {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// WorstSeverity is the worst severity in a set of advisories, or "" when there are none.
//
// Exported so a caller can decide how loudly to say something without re-implementing the ranking —
// and so "is this critical?" has one answer rather than one per surface.
func WorstSeverity(advisories []Advisory) string {
	worst := ""
	for _, a := range advisories {
		if severityRank(a.Severity) > severityRank(worst) {
			worst = a.Severity
		}
	}
	return worst
}
