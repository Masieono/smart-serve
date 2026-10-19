package interp

import (
	"fmt"
	"time"

	"github.com/masieono/smart-serve/internal/model"
)

// sectorAttributeIDs are the attributes whose raw value counts storage the drive has lost and
// cannot get back: reallocated (5), pending (197) and uncorrectable (198).
//
// Named once so a caller can ask about a single attribute — "did one of *these* move?" — without
// keeping its own copy of the list. A second copy is a second thing to forget to update.
var sectorAttributeIDs = []int{5, 197, 198}

// IsSectorAttribute reports whether an attribute ID is one of the lost-storage counters.
func IsSectorAttribute(id int) bool {
	for _, candidate := range sectorAttributeIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

// SectorIssues totals the three attributes that mean "this drive has lost storage it
// cannot get back": reallocated (5), pending (197) and uncorrectable (198).
//
// Summed rather than tracked separately because the question that matters over time is
// whether the number is *moving*, and all three answer it.
func SectorIssues(attrs []model.Attribute) int64 {
	var total int64
	for _, id := range sectorAttributeIDs {
		for i := range attrs {
			if attrs[i].ID == id {
				total += int64(effectiveCount(attrs[i].RawValue))
				break
			}
		}
	}
	return total
}

// Observation is one snapshot reduced to the few numbers a projection needs. Callers
// build these from stored history, which keeps this package free of storage types.
type Observation struct {
	TS time.Time
	// WearPct is the percentage of rated life already used, where the drive reports it
	// (NVMe percentage_used, or an ATA wear indicator).
	WearPct *float64
	// Hours is power-on hours, used to sanity-check a rate against real use.
	Hours *int64
	// Sectors is SectorIssues at that moment.
	Sectors *int64
}

// Projection is what the history says about where a drive is heading.
//
// It is deliberately modest. A S.M.A.R.T. reading is a snapshot of a device that does
// not know its own future, so this reports a rate and a horizon with the confidence the
// data supports, and says what it is not. The failure mode to avoid is a confident date
// computed from two points.
type Projection struct {
	// Basis names what the summary is built from: "wear", "sectors" or "none".
	Basis string `json:"basis"`
	// Summary is the one-line answer.
	Summary string `json:"summary"`
	// Confidence is "high", "medium" or "low", or "" when there is no basis.
	Confidence string `json:"confidence,omitempty"`
	// Caveat says what the number is not. Always set when there is a summary.
	Caveat string `json:"caveat,omitempty"`
	// Worsening is non-empty when something is measurably getting worse, and says what.
	// It looks only at the recent window (WorsenWindow), not at the whole history: a
	// drive that got worse months ago and has been stable since is not getting worse.
	Worsening string `json:"worsening,omitempty"`
}

// Window thresholds. The observation span has to be long enough that a rate means
// something: two scans an hour apart say almost nothing about a drive's remaining life.
const (
	wearMinSpan   = 24 * time.Hour
	wearGoodSpan  = 30 * 24 * time.Hour
	sectorMinSpan = 24 * time.Hour
	wearMinDelta  = 0.5 // % — below this, the change is inside the drive's own rounding
)

// Project reduces a drive's history to a longevity estimate and a worsening flag.
//
// `obs` is oldest-first and may be empty. Wear is preferred over sectors when both are
// available: it is a manufactured counter with a known scale, whereas sector counts are
// bursty and a rate taken through two of them is close to meaningless.
func Project(obs []Observation) Projection {
	out := Projection{Basis: "none"}

	// Worsening is reported independently of which basis the summary uses, because a
	// drive can be wearing normally *and* suddenly throwing new bad sectors.
	out.Worsening = worsening(obs)

	if p, ok := wearProjection(obs); ok {
		return merge(out, p)
	}
	if p, ok := sectorProjection(obs); ok {
		return merge(out, p)
	}
	out.Summary = "Not enough history yet"
	out.Caveat = "Trends appear once this drive has been scanned a few times over at least a day."
	return out
}

func merge(base, add Projection) Projection {
	base.Basis = add.Basis
	base.Summary = add.Summary
	base.Confidence = add.Confidence
	base.Caveat = add.Caveat
	return base
}

// wearProjection extrapolates a drive's own wear counter. It only trusts a rate taken
// across a span long enough for the counter to have moved for reasons other than
// rounding.
func wearProjection(obs []Observation) (Projection, bool) {
	points := withWear(obs)
	if len(points) < 2 {
		return Projection{}, false
	}
	first, last := points[0], points[len(points)-1]
	span := last.TS.Sub(first.TS)
	if span < wearMinSpan {
		return Projection{}, false
	}

	used := *last.WearPct
	delta := used - *first.WearPct
	days := span.Hours() / 24

	if delta < wearMinDelta {
		return Projection{
			Basis:      "wear",
			Summary:    fmt.Sprintf("Wear has not measurably increased in %s", humanDays(days)),
			Confidence: "medium",
			Caveat: "The counter moves in whole percent, so a slow but real trend can hide " +
				"below a single point over this window.",
		}, true
	}

	perDay := delta / days
	remaining := 100 - used
	if remaining < 0 {
		remaining = 0
	}
	daysLeft := remaining / perDay

	confidence := "low"
	caveat := "Extrapolated from two readings. Wear is rarely linear — many drives hold " +
		"a flat rate and then fall away at the end of life."
	if span >= wearGoodSpan {
		confidence = "medium"
		caveat = "Extrapolated from the observed rate over this window. Wear is rarely " +
			"linear, and the rating is the manufacturer's, not a measurement."
	}

	return Projection{
		Basis:      "wear",
		Summary:    fmt.Sprintf("%s of rated life left, about %s at the current rate", pct(remaining), humanDays(daysLeft)),
		Confidence: confidence,
		Caveat:     caveat,
	}, true
}

// sectorProjection reports whether bad sectors are accumulating. It deliberately does
// not project a date: sector growth arrives in bursts, and a rate taken through two
// bursts produces a number that looks precise and means nothing.
func sectorProjection(obs []Observation) (Projection, bool) {
	points := withSectors(obs)
	if len(points) < 2 {
		return Projection{}, false
	}
	first, last := points[0], points[len(points)-1]
	span := last.TS.Sub(first.TS)
	if span < sectorMinSpan {
		return Projection{}, false
	}
	days := span.Hours() / 24
	delta := *last.Sectors - *first.Sectors

	if delta <= 0 {
		return Projection{
			Basis:      "sectors",
			Summary:    fmt.Sprintf("No new sector problems in %s", humanDays(days)),
			Confidence: "medium",
			Caveat: "A clean window is the absence of bad news, not a forecast. Drives " +
				"often fail without warning after a long quiet period.",
		}, true
	}

	return Projection{
		Basis:      "sectors",
		Summary:    fmt.Sprintf("%d new sector problems in %s", delta, humanDays(days)),
		Confidence: "low",
		Caveat: "Sector counts arrive in bursts rather than at a steady rate, so this is " +
			"a direction rather than a date. A drive whose count is moving is worth " +
			"replacing before it stops moving at all.",
	}, true
}

// WorsenWindow is how far back the worsening badge looks, and it is deliberately not the
// whole observation window.
//
// A badge that stays lit for months after a single burst of bad sectors trains people to
// ignore it, so the comparison is against the newest reading at least this old: once a
// drive stops getting worse, the badge clears itself. A drive can be worse than it was six
// months ago and still not be getting worse today, and only the second one is an alarm.
//
// The summary keeps describing the whole window — "7 new sector problems in 180 days" is a
// true statement about the history. The badge answers the narrower question "is this
// getting worse now", which is why the two can disagree, and should.
const WorsenWindow = 30 * 24 * time.Hour

// HistoryWindow is how far back a question about history may look.
//
// It deliberately matches the default `retention_days` setting: snapshots older than that have been
// pruned, so a longer window would be a promise the database cannot keep. A user who lowers
// retention gets a shorter window, which is correct — a trend is limited by the history that
// exists, not by this constant.
const HistoryWindow = 180 * 24 * time.Hour

// TrendPenaltyCap is the most a worsening trend can take off a score.
//
// Bounded deliberately. A trend is an extrapolation from a handful of readings, and a score is the
// number people quote and compare; letting an extrapolation move it by more than a few points would
// make the score say more than the data supports. It is also small enough that the trend alone can
// never carry a drive out of its verdict — the trend adjusts the summary, it does not overrule the
// drive's own data.
const TrendPenaltyCap = 10

// TrendPenalty is how many points a worsening trend takes off a score (`FEATURES.md` A17).
//
// Any growth costs something, because any growth is a finding: a drive that has reallocated even one
// sector has lost storage it cannot get back, and the count going *up* is the difference between a
// drive to watch and a drive to watch closely.
//
// Past that it scales by doubling rather than linearly, and stops at a cap. Sector counts arrive in
// bursts, so the difference between 200 and 400 new sectors is not the difference between two
// scores — and a rule that kept growing would let a single bad afternoon dominate the number.
func TrendPenalty(sectorsAdded int64) int {
	if sectorsAdded <= 0 {
		return 0
	}
	penalty := 3
	for step := int64(2); step <= sectorsAdded && penalty < TrendPenaltyCap; step *= 2 {
		penalty++
	}
	return penalty
}

// worsening is the early-warning flag: something is measurably getting worse *now*.
//
// It compares the latest reading against the newest one at least WorsenWindow older,
// falling back to the oldest reading when the whole history is younger than that — a fresh
// install whose drive is already shedding sectors has to be told, even though there is not
// yet a month of history to compare against.
func worsening(obs []Observation) string {
	points := withSectors(obs)
	if len(points) < 2 {
		return ""
	}
	last := points[len(points)-1]

	// Points are oldest-first, so scanning backwards finds the *newest* reading that is
	// old enough to compare against.
	base := points[0]
	for i := len(points) - 2; i >= 0; i-- {
		if last.TS.Sub(points[i].TS) >= WorsenWindow {
			base = points[i]
			break
		}
	}

	delta := *last.Sectors - *base.Sectors
	if delta <= 0 {
		return ""
	}
	days := last.TS.Sub(base.TS).Hours() / 24
	return fmt.Sprintf("%d new sector problems in %s", delta, humanDays(days))
}

func withWear(obs []Observation) []Observation {
	out := make([]Observation, 0, len(obs))
	for _, o := range obs {
		if o.WearPct != nil {
			out = append(out, o)
		}
	}
	return out
}

func withSectors(obs []Observation) []Observation {
	out := make([]Observation, 0, len(obs))
	for _, o := range obs {
		if o.Sectors != nil {
			out = append(out, o)
		}
	}
	return out
}

// humanDays renders a horizon without pretending to more precision than it has.
//
// A year is the boundary, and it has to be: the first version switched at 60 days and
// turned a five-month horizon into "0.4 years", and the second switched at 180 and turned
// ten months into "0.9 years". Below a year, days read better; above it, years do.
func humanDays(days float64) string {
	switch {
	case days < 1:
		return "less than a day"
	case days < 365:
		return fmt.Sprintf("%.0f days", days)
	case days < 3650:
		return fmt.Sprintf("%.1f years", days/365)
	default:
		return fmt.Sprintf("%.0f years", days/365)
	}
}

func pct(v float64) string {
	if v >= 10 || v == 0 {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}
