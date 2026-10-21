package knowledge

import (
	"strings"
	"testing"
)

// The bundled table is data, and data has typos. This is the test that catches them: a shipped file
// that does not parse would leave every install with an empty knowledge base, and the only symptom
// would be the app saying nothing about every drive — which looks exactly like working correctly.
func TestTheBundledTableIsValid(t *testing.T) {
	b, err := Embedded()
	if err != nil {
		t.Fatalf("the shipped knowledge base does not parse: %v", err)
	}
	if len(b.Drives) < 5 {
		t.Errorf("the shipped table has %d entries, which is too few to be worth bundling", len(b.Drives))
	}
	// Every entry has to say where its figures came from. A number nobody can check is a number
	// nobody should believe, and the app shows this string beside the figure.
	for _, e := range b.Drives {
		if strings.TrimSpace(e.Source) == "" {
			t.Errorf("%s has no source", e.Family)
		}
	}
	// And the round trip has to be byte-stable, because this file *is* the published one: a
	// contributor edits it, and the refresh URL points at it.
	if _, err := Parse(EmbeddedBytes()); err != nil {
		t.Errorf("the raw bytes do not parse: %v", err)
	}
}

func entry(family string, tbw map[string]float64) Entry {
	e := Entry{Family: family}
	if tbw != nil {
		e.RatedTBWByCapacityGB = tbw
	}
	return e
}

func gb(n uint64) *uint64 { return &n }

// Matching is a substring test because model strings are messy: the app sees "WDC WD40EFRX-68N32N0"
// as a model and often no family at all.
func TestFindMatchesOnModelOrFamily(t *testing.T) {
	base := Base{Version: FormatVersion, Drives: []Entry{
		entry("Samsung SSD 870 EVO", map[string]float64{"1000": 600}),
	}}

	for _, tc := range []struct{ model, family string }{
		{"Samsung SSD 870 EVO 1TB", "Samsung SSD 870 EVO"},
		{"Samsung SSD 870 EVO", ""},
		{"samsung ssd 870 evo 500GB", ""}, // case-insensitive
		{"", "Samsung SSD 870 EVO"},
	} {
		got := base.Find(tc.model, tc.family, "", gb(1000204886016))
		if !got.Known() {
			t.Errorf("no match for model=%q family=%q", tc.model, tc.family)
		}
	}

	// A different family is not a match, and neither is a partial word.
	for _, tc := range []struct{ model, family string }{
		{"Samsung SSD 860 EVO 1TB", ""},
		{"Samsung SSD 87", ""},
		{"", ""},
	} {
		if got := base.Find(tc.model, tc.family, "", nil); got.Known() {
			t.Errorf("model=%q matched %q, want nothing", tc.model, got.Family)
		}
	}
}

// A specific entry beats a general one whatever order they appear in the file, so adding a general
// entry cannot silently shadow a precise one.
func TestTheMostSpecificEntryWins(t *testing.T) {
	general := entry("Samsung SSD", map[string]float64{"1000": 1})
	specific := entry("Samsung SSD 870 EVO", map[string]float64{"1000": 600})

	for _, drives := range [][]Entry{{general, specific}, {specific, general}} {
		base := Base{Version: FormatVersion, Drives: drives}
		got := base.Find("Samsung SSD 870 EVO 1TB", "", "", gb(1000204886016))
		if got.Family != "Samsung SSD 870 EVO" {
			t.Errorf("matched %q, want the specific entry", got.Family)
		}
		if got.RatedTBW == nil || *got.RatedTBW != 600 {
			t.Errorf("rated = %v, want 600", got.RatedTBW)
		}
	}
}

// A pattern short enough to match unrelated drives is refused outright, because a wrong entry is
// worse than a missing one: it carries a number that looks authoritative.
func TestAShortPatternMatchesNothing(t *testing.T) {
	base := Base{Version: FormatVersion, Drives: []Entry{{Family: "EVO", RatedTBW: ptr(600.0)}}}
	if got := base.Find("Samsung SSD 870 EVO 1TB", "", "", nil); got.Known() {
		t.Errorf("a four-character family matched: %+v", got)
	}
}

// `matches` is for a series that shares a suffix rather than a name — the SMR Red line, whose models
// are WD20EFAX through WD80EFAX. Four characters at the end is too short to match on safely by itself,
// and no single family string can express it.
func TestMatchesPatternsCoverASeries(t *testing.T) {
	base := Base{Version: FormatVersion, Drives: []Entry{{
		Family:  "WD Red (EFAX, SMR)",
		Matches: []string{"WD20EFAX", "WD40EFAX", "WD60EFAX"},
		Notes:   "SMR",
	}}}

	if got := base.Find("WDC WD40EFAX-68JH4N0", "", "", nil); got.Family != "WD Red (EFAX, SMR)" {
		t.Errorf("matched %q, want the EFAX series entry", got.Family)
	}
	// And a drive that is *not* in the series is not swept up by the suffix.
	if got := base.Find("WDC WD40EFRX-68N32N0", "", "", nil); got.Known() {
		t.Errorf("an EFRX matched the EFAX entry: %+v", got)
	}
}

func ptr[T any](v T) *T { return &v }

// Endurance is per capacity, because a 250 GB and a 4 TB drive in one family are rated differently —
// and quoting the family's headline number for both would be wrong for one of them.
func TestEnduranceResolvesForTheDrivesCapacity(t *testing.T) {
	e := entry("Samsung SSD 870 EVO", map[string]float64{"250": 150, "500": 300, "1000": 600, "4000": 2400})
	base := Base{Version: FormatVersion, Drives: []Entry{e}}

	for _, tc := range []struct {
		name   string
		bytes  *uint64
		want   float64
		approx bool
	}{
		{"1 TB", gb(1000204886016), 600, false},
		{"500 GB", gb(500107862016), 300, false},
		{"250 GB", gb(250059350016), 150, false},
		{"4 TB", gb(4000787030016), 2400, false},
	} {
		got := base.Find("Samsung SSD 870 EVO 1TB", "", "", tc.bytes)
		if got.RatedTBW == nil {
			t.Errorf("%s: no figure", tc.name)
			continue
		}
		if *got.RatedTBW != tc.want {
			t.Errorf("%s: rated = %v, want %v", tc.name, *got.RatedTBW, tc.want)
		}
		if got.RatedTBWApprox != tc.approx {
			t.Errorf("%s: approx = %v, want %v", tc.name, got.RatedTBWApprox, tc.approx)
		}
	}
}

// A capacity the table does not cover says nothing rather than quoting a neighbour's figure: a rating
// for a different capacity is a number about a different drive, and inventing one is the failure this
// whole table exists to avoid.
func TestAnUnknownCapacitySaysNothing(t *testing.T) {
	e := entry("Samsung SSD 870 EVO", map[string]float64{"250": 150, "1000": 600})
	base := Base{Version: FormatVersion, Drives: []Entry{e}}

	// A 3 TB drive is nowhere near either bucket.
	if got := base.Find("Samsung SSD 870 EVO", "", "", gb(3000592982016)); got.RatedTBW != nil {
		t.Errorf("rated = %v for an uncovered capacity, want nothing", *got.RatedTBW)
	}
	// The entry is still matched, so the app can say it knows the family and not the figure.
	got := base.Find("Samsung SSD 870 EVO", "", "", gb(3000592982016))
	if !got.Known() || got.Family != "Samsung SSD 870 EVO" {
		t.Errorf("lookup = %+v, want the family matched with no figure", got)
	}

	// A drive that reports no capacity falls back to the flat figure, and says so.
	flat := Entry{Family: "Some SSD", RatedTBW: ptr(400.0)}
	base = Base{Version: FormatVersion, Drives: []Entry{flat}}
	if got := base.Find("Some SSD 1TB", "", "", nil); got.RatedTBW == nil || *got.RatedTBW != 400 {
		t.Errorf("rated = %v, want the flat figure", got.RatedTBW)
	}
	// And with neither a capacity nor a flat figure, nothing.
	byCapacity := entry("Other SSD", map[string]float64{"1000": 600})
	base = Base{Version: FormatVersion, Drives: []Entry{byCapacity}}
	if got := base.Find("Other SSD", "", "", nil); got.RatedTBW != nil {
		t.Errorf("rated = %v with no capacity to look up, want nothing", *got.RatedTBW)
	}
}

// An advisory names the firmware it affects. One that names none applies to the whole family — which
// is how the SMR Red line is written, because the problem is the model rather than a revision.
func TestAdvisoriesMatchOnFirmware(t *testing.T) {
	e := Entry{Family: "Test Drive", Advisories: []Advisory{
		{Firmware: "AAA1", Severity: SeverityWarning, Title: "old firmware"},
		{Firmware: "BBB2, CCC3", Severity: SeverityCritical, Title: "two revisions"},
		{Severity: SeverityInfo, Title: "whole family"},
	}}
	base := Base{Version: FormatVersion, Drives: []Entry{e}}

	// A drive on the affected firmware gets both the specific advisory and the family-wide one, worst
	// first so the one that matters is read.
	got := base.Find("Test Drive", "", "AAA1", nil)
	if len(got.Advisories) != 2 {
		t.Fatalf("advisories = %+v, want two", got.Advisories)
	}
	if got.Advisories[0].Title != "old firmware" {
		t.Errorf("first advisory = %q, want the warning before the info", got.Advisories[0].Title)
	}

	// A comma-separated list covers every revision named in it.
	if got := base.Find("Test Drive", "", "ccc3", nil); len(got.Advisories) != 2 {
		t.Errorf("a listed revision did not match: %+v", got.Advisories)
	}

	// A revision nobody flagged gets only the family-wide advisory.
	if got := base.Find("Test Drive", "", "ZZZ9", nil); len(got.Advisories) != 1 ||
		got.Advisories[0].Title != "whole family" {
		t.Errorf("advisories = %+v, want only the family-wide one", got.Advisories)
	}

	// A drive that reported no firmware cannot match a firmware-specific advisory — that would be
	// guessing at the one fact the advisory is about — but the family-wide one still applies.
	got = base.Find("Test Drive", "", "", nil)
	if len(got.Advisories) != 1 || got.Advisories[0].Title != "whole family" {
		t.Errorf("advisories with no firmware = %+v, want only the family-wide one", got.Advisories)
	}
}

func TestWorstSeverity(t *testing.T) {
	if got := WorstSeverity(nil); got != "" {
		t.Errorf("worst = %q, want empty", got)
	}
	got := WorstSeverity([]Advisory{
		{Severity: SeverityInfo},
		{Severity: SeverityCritical},
		{Severity: SeverityWarning},
	})
	if got != SeverityCritical {
		t.Errorf("worst = %q, want critical", got)
	}
}

// Parsing is strict, and the source is why: this file arrives from a URL or a text box, and an entry
// silently dropped for a typo looks *exactly* like the app not knowing a drive — the one failure a
// knowledge base cannot afford, because it is indistinguishable from having no data at all.
func TestParseRefusesWhatItCannotUse(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want string
	}{
		{"not JSON", `{`, "not a knowledge document"},
		{"unknown field", `{"version":1,"drives":[{"family":"Some SSD","rated_tbw":1,"typo":2}]}`,
			"unknown field"},
		{"wrong version", `{"version":99,"drives":[{"family":"Some SSD","rated_tbw":1}]}`,
			"unsupported format version"},
		{"no drives", `{"version":1,"drives":[]}`, "no drives"},
		{"missing family", `{"version":1,"drives":[{"rated_tbw":1}]}`, "family is required"},
		{"short family", `{"version":1,"drives":[{"family":"EVO","rated_tbw":1}]}`,
			"shorter than 5 characters"},
		{"short match pattern",
			`{"version":1,"drives":[{"family":"Some SSD","matches":["FAX"],"rated_tbw":1}]}`,
			"shorter than 5 characters"},
		{"duplicate family",
			`{"version":1,"drives":[{"family":"Some SSD","rated_tbw":1},{"family":"some ssd","rated_tbw":2}]}`,
			"appears twice"},
		{"nothing to say", `{"version":1,"drives":[{"family":"Some SSD"}]}`, "nothing to say"},
		{"zero rating", `{"version":1,"drives":[{"family":"Some SSD","rated_tbw":0}]}`,
			"must be positive"},
		{"bad capacity key",
			`{"version":1,"drives":[{"family":"Some SSD","rated_tbw_by_capacity_gb":{"1TB":600}}]}`,
			"not a capacity in GB"},
		{"advisory without a title",
			`{"version":1,"drives":[{"family":"Some SSD","advisories":[{"severity":"info"}]}]}`,
			"title is required"},
		{"advisory with an unknown severity",
			`{"version":1,"drives":[{"family":"Some SSD","advisories":[{"severity":"spicy","title":"x"}]}]}`,
			"severity must be info, warning or critical"},
	} {
		_, err := Parse([]byte(tc.doc))
		if err == nil {
			t.Errorf("%s: parsed without complaint", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %q, want it to mention %q", tc.name, err, tc.want)
		}
	}

	// And a valid one gets through.
	good := `{"version":1,"updated":"2026-01-01","drives":[
		{"family":"Some SSD","rated_tbw_by_capacity_gb":{"1000":600},"source":"datasheet",
		 "advisories":[{"severity":"warning","title":"a problem","detail":"what to do"}]}]}`
	if _, err := Parse([]byte(good)); err != nil {
		t.Errorf("a valid document was refused: %v", err)
	}
}

// The layers are bundled, then fetched, then the user's overrides — and an entry *replaces* the one
// for the same family rather than merging field by field. Field-level merging would make it
// impossible to remove an advisory, and "I deleted it and it is still there" is a bug report nobody
// can act on.
func TestMergeLayersByFamily(t *testing.T) {
	bundled := Base{Version: FormatVersion, Drives: []Entry{
		entry("Samsung SSD 870 EVO", map[string]float64{"1000": 600}),
		entry("Crucial MX500", map[string]float64{"1000": 360}),
	}}
	fetched := Base{Version: FormatVersion, Updated: "2026-06-01", Drives: []Entry{
		entry("Samsung SSD 870 EVO", map[string]float64{"1000": 650}), // corrected
		entry("Samsung SSD 990 PRO", map[string]float64{"1000": 600}), // new
	}}
	overrides := Base{Version: FormatVersion, Drives: []Entry{
		{Family: "Crucial MX500", RatedTBW: ptr(999.0)}, // the user's own figure
	}}

	got := Merge(bundled, fetched, overrides)

	byFamily := map[string]Entry{}
	for _, e := range got.Drives {
		byFamily[e.Family] = e
	}
	if len(got.Drives) != 3 {
		t.Errorf("merged %d entries, want 3 (one replaced, one added): %+v", len(got.Drives), got.Drives)
	}
	if e := byFamily["Samsung SSD 870 EVO"]; e.RatedTBWByCapacityGB["1000"] != 650 {
		t.Errorf("the fetched correction did not win: %+v", e)
	}
	if e := byFamily["Crucial MX500"]; e.RatedTBW == nil || *e.RatedTBW != 999 {
		t.Errorf("the override did not win: %+v", e)
	}
	// The replacement is whole-entry, so the bundled per-capacity table is gone rather than merged.
	if e := byFamily["Crucial MX500"]; e.RatedTBWByCapacityGB != nil {
		t.Errorf("the override merged field by field instead of replacing: %+v", e)
	}
	if byFamily["Samsung SSD 990 PRO"].RatedTBWByCapacityGB["1000"] != 600 {
		t.Error("a new entry was dropped")
	}
	if got.Updated != "2026-06-01" {
		t.Errorf("updated = %q, want the newest layer's date", got.Updated)
	}

	// The result is sorted, so the same layers always produce the same document — which matters
	// because the overrides editor hands the user a copy of it to edit.
	for i := 1; i < len(got.Drives); i++ {
		if got.Drives[i-1].Family > got.Drives[i].Family {
			t.Errorf("entries are not sorted: %+v", got.Drives)
		}
	}

	// And a merged document is still valid, which is what makes it safe to hand back out.
	if err := got.Validate(); err != nil {
		t.Errorf("the merged document does not validate: %v", err)
	}
}

// Matching by family is case-insensitive on the *entry's* side too, so a merge keyed on "some ssd" and
// one keyed on "Some SSD" are one entry rather than two.
func TestMergeIsCaseInsensitiveOnFamily(t *testing.T) {
	a := Base{Version: FormatVersion, Drives: []Entry{{Family: "Some SSD", RatedTBW: ptr(1.0)}}}
	b := Base{Version: FormatVersion, Drives: []Entry{{Family: "SOME SSD", RatedTBW: ptr(2.0)}}}

	got := Merge(a, b)
	if len(got.Drives) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got.Drives), got.Drives)
	}
	if *got.Drives[0].RatedTBW != 2 {
		t.Errorf("rated = %v, want the later layer", *got.Drives[0].RatedTBW)
	}
}
