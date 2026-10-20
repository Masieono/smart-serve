package store

import "strings"

// BenchTag marks a drive as being on the bench rather than in the fleet (`FEATURES.md` D11).
//
// A tag rather than a new column, and that is the design rather than a shortcut. Tags already group
// (`D9`), survive an unplug, export and import (`C7`), and appear in every report — so "label the
// batch" needs no storage, no migration and no new control, and the feature's own words are exactly
// that: *label, test and report a batch of bench drives*. What the tag *means* is the app's contract
// with the user, which is why it is named here rather than spelled out at each use.
//
// A drive on the bench is one being evaluated, so it is left out of the fleet's health summary and
// out of the replacement ranking: a bench drive that fails is a **result**, not an alarm, and a
// batch of five failing drives would otherwise dominate both. It is never left out of the list or an
// export — being out of the summary is not the same as being hidden.
const BenchTag = "bench"

// TagList splits a drive's stored tag string into its tags: comma-separated, trimmed, blanks
// dropped.
//
// The storage is one free-text field rather than a join table, so the rule for reading it lives here
// instead of at each caller. The UI splits it the same way (`assets/js/views/dashboard.js`) and the
// two have to agree, or a drive would appear in a group the export does not know about.
func TagList(tags string) []string {
	var out []string
	for _, t := range strings.Split(tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// HasTag reports whether a drive's stored tags include want, ignoring case.
//
// Case-insensitive because the tags are typed by hand and "Bench" is the same intention as "bench";
// the grouping in the UI already compares case-insensitively for the same reason.
func HasTag(tags, want string) bool {
	for _, t := range TagList(tags) {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}
