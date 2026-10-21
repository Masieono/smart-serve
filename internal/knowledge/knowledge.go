// Package knowledge holds the drive knowledge base (`FEATURES.md` A21): rated endurance per model
// family, and known firmware advisories.
//
// **One format, three sources.** The same JSON document is bundled with the binary, fetched from a
// configurable URL, and typed into the overrides editor — so a contributor edits exactly the shape the
// app reads, and there is one thing to learn rather than three. This package owns that shape, the
// validation, and the matching; where the bytes came from is the caller's business.
//
// The data is *published but not machine-readable* (`ADR-023`): rated endurance lives in datasheets
// and support pages, so a curated table is the only honest source. Every entry therefore carries the
// `source` it came from — a number nobody can check is a number nobody should believe, and the app
// shows it wherever it shows the figure.
package knowledge

// Severity of a firmware advisory, worst last.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Base is one knowledge document.
type Base struct {
	// Version is the format marker, checked on load so a file written for a later version is refused
	// rather than half-understood.
	Version int `json:"version"`
	// Updated is when the data was last reviewed, as text. Not a time.Time: it is a human note about
	// a datasheet, not a machine timestamp, and parsing it would invite it to be wrong.
	Updated string `json:"updated,omitempty"`
	// Note is free text carried with the document, so a file found on its own explains itself.
	Note   string  `json:"note,omitempty"`
	Drives []Entry `json:"drives"`
}

// Entry is what the base knows about one model family.
type Entry struct {
	// Family is the entry's name: it is what the app shows, what the overrides editor keys on, and —
	// unless `matches` says otherwise — the string matched against a drive's model and model family.
	//
	// It is matched case-insensitively as a *substring*, because model strings are messy: "WDC
	// WD40EFRX-68N32N0" is a WD40EFRX, and no two vendors punctuate the same way.
	Family string `json:"family"`
	// Matches are additional patterns, for a series that shares a suffix rather than a name. The SMR
	// Red line is the reason this exists: those drives are "WDC WD40EFAX-…", "WDC WD60EFAX-…" and so
	// on, so the thing they have in common is four characters at the end — which is too short to match
	// on safely by itself, and which no single `family` string can express.
	//
	// When set, these replace `family` as the match patterns; `family` stays the entry's name.
	Matches []string `json:"matches,omitempty"`
	// RatedTBW is the endurance in terabytes written, when it does not depend on capacity.
	RatedTBW *float64 `json:"rated_tbw,omitempty"`
	// RatedTBWByCapacityGB is the same figure per capacity, because it usually *does* depend on
	// capacity: a 250 GB and a 4 TB drive in one family are rated differently, and quoting the
	// family's headline number for both would be wrong for one of them. Keyed by the decimal GB the
	// drive reports — 500, 512, 1000, 1024 — rounded to the nearest whole number.
	RatedTBWByCapacityGB map[string]float64 `json:"rated_tbw_by_capacity_gb,omitempty"`
	// RatedWorkloadTBPerYear is the rotating-drive equivalent: the workload the vendor rates it for.
	// Separate from TBW because they are different promises — one is a total, the other a rate — and
	// adding them would let an HDD's rating be read as an SSD's.
	RatedWorkloadTBPerYear *float64 `json:"rated_workload_tb_per_year,omitempty"`
	// Source is where the figure came from, shown wherever the figure is.
	Source string `json:"source,omitempty"`
	// Notes is anything a reader should know that is not a number.
	Notes      string     `json:"notes,omitempty"`
	Advisories []Advisory `json:"advisories,omitempty"`
}

// Advisory is a known problem with a firmware version.
type Advisory struct {
	// Firmware is the affected version, matched case-insensitively and exactly. Empty means "every
	// firmware of this family", which is how a family-wide advisory is written without inventing a
	// wildcard syntax.
	Firmware string `json:"firmware,omitempty"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	// Detail says what it does and what to do about it. A title alone tells a reader they have a
	// problem and not what to do, which is worse than saying nothing.
	Detail string `json:"detail,omitempty"`
	URL    string `json:"url,omitempty"`
}

// Lookup is what the base knows about one particular drive.
type Lookup struct {
	// Entry is the matched family, nil when nothing matched.
	Entry *Entry `json:"-"`
	// Family is the matched family's name, empty when nothing matched.
	Family string `json:"family,omitempty"`
	// RatedTBW is resolved for *this* drive's capacity, so a caller never has to do the lookup — or
	// forget to.
	RatedTBW *float64 `json:"rated_tbw,omitempty"`
	// RatedTBWApprox is true when the figure came from a capacity bucket that only nearly matches, so
	// the app can say "about" rather than stating a number it does not have.
	RatedTBWApprox bool `json:"rated_tbw_approx,omitempty"`
	// RatedWorkloadTBPerYear is the vendor's rated workload for a rotating drive.
	RatedWorkloadTBPerYear *float64 `json:"rated_workload_tb_per_year,omitempty"`
	Source                 string   `json:"source,omitempty"`
	Notes                  string   `json:"notes,omitempty"`
	// Advisories are the ones that apply to this drive: those naming its firmware, plus those naming
	// none.
	Advisories []Advisory `json:"advisories,omitempty"`
}

// Known reports whether the base had anything to say about the drive.
func (l Lookup) Known() bool {
	return l.Entry != nil
}
