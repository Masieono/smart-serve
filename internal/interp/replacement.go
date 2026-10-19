package interp

import (
	"fmt"
	"sort"

	"github.com/masieono/smart-serve/internal/model"
)

// ReplacementBand is how strongly a drive argues for replacement (`FEATURES.md` A24).
type ReplacementBand string

const (
	// ReplaceNow is a drive that has failed, or whose write endurance is nearly spent.
	ReplaceNow ReplacementBand = "now"
	// ReplaceSoon is a drive that is measurably deteriorating, or well into its rated writes.
	ReplaceSoon ReplacementBand = "soon"
	// ReplaceWatch is a drive worth watching: caution, or a count that is moving.
	ReplaceWatch ReplacementBand = "watch"
	// ReplaceFine is a drive with nothing against it.
	ReplaceFine ReplacementBand = "fine"
)

// ReplacementCandidate is one drive's inputs to the ranking.
type ReplacementCandidate struct {
	Serial    string
	Name      string
	Health    model.Verdict
	Rationale string
	Score     int
	// Worsening is how many bad sectors appeared over the recent window.
	Worsening int64
	// WearPct is the percentage of rated write endurance used, nil for a drive that reports none.
	// It is "used", not "left", because that is what the drives report and inverting it twice is
	// how a sign gets lost.
	WearPct *float64
}

// ReplacementRank is one drive's place in the ranking.
type ReplacementRank struct {
	Serial string          `json:"serial"`
	Name   string          `json:"name,omitempty"`
	Band   ReplacementBand `json:"band"`
	// Reasons are the facts that put the drive in its band, in the drive's own terms. They are the
	// point of the ranking: a reader can disagree with a band, and cannot disagree with a number.
	Reasons []string `json:"reasons"`
}

// Where a band sits relative to the others, so the reasons can only ever raise it.
type severity int

const (
	sevFine severity = iota
	sevWatch
	sevSoon
	sevNow
)

var bandNames = map[severity]ReplacementBand{
	sevFine:  ReplaceFine,
	sevWatch: ReplaceWatch,
	sevSoon:  ReplaceSoon,
	sevNow:   ReplaceNow,
}

// Thresholds for the write-endurance half of the rule.
//
// Named rather than inline because they are the only numbers in this file, and they are the part
// most likely to be argued with. A drive at 90% of its rated writes is near the end of its design
// life whatever its verdict says; at 75% it is worth planning for.
const (
	replaceNowWear  = 90.0
	replaceSoonWear = 75.0
)

// RankForReplacement orders drives by how strongly each argues for replacement (`FEATURES.md` A24).
//
// The dashboard already sorts by *today's* severity. This answers a different question — "which one
// do I buy this month?" — which is about direction and remaining life as much as about the verdict.
//
// Bands rather than a weighted sum, deliberately. A weighted sum would be a number nobody could
// argue with or explain, and the whole point of this ranking is that it shows its reasons: "failing
// now", "7 new sector problems in 30 days", "94% of its rated write endurance used". A reader can
// disagree with a band and act on it; they cannot do either with an unexplained 63.4.
//
// Every drive is returned, including the healthy ones, so a caller can decide how much of the
// ranking to show. Within a band the order is by score ascending — the worst of the bad first — and
// then by serial, so the list is stable between renders.
func RankForReplacement(candidates []ReplacementCandidate) []ReplacementRank {
	ranks := make([]ReplacementRank, 0, len(candidates))
	for _, c := range candidates {
		band, reasons := bandFor(c)
		ranks = append(ranks, ReplacementRank{
			Serial: c.Serial, Name: c.Name, Band: band, Reasons: reasons,
		})
	}

	bySeverity := map[ReplacementBand]severity{}
	for sev, band := range bandNames {
		bySeverity[band] = sev
	}
	scoreOf := make(map[string]int, len(candidates))
	for _, c := range candidates {
		scoreOf[c.Serial] = c.Score
	}

	sort.SliceStable(ranks, func(i, j int) bool {
		a, b := bySeverity[ranks[i].Band], bySeverity[ranks[j].Band]
		if a != b {
			return a > b
		}
		// A drive with no score sorts last within its band: -1 means "not scored", which is not a
		// worse score than 0.
		sa, sb := scoreOf[ranks[i].Serial], scoreOf[ranks[j].Serial]
		if (sa < 0) != (sb < 0) {
			return sb < 0
		}
		if sa != sb {
			return sa < sb
		}
		return ranks[i].Serial < ranks[j].Serial
	})
	return ranks
}

// bandFor is the rule itself: which band a drive belongs in, and why.
func bandFor(c ReplacementCandidate) (ReplacementBand, []string) {
	sev := sevFine
	var reasons []string

	switch c.Health {
	case model.Bad:
		sev = sevNow
		reasons = append(reasons, rationaleOr(c.Rationale, "failing now"))
	case model.Caution:
		sev = max(sev, sevWatch)
		reasons = append(reasons, rationaleOr(c.Rationale, "caution"))
	}

	// Deteriorating is a stronger signal than merely *being* in a bad state: a drive that is green
	// today and moving is the one that surprises you, and the verdict alone cannot say so.
	if c.Worsening > 0 {
		sev = max(sev, sevWatch)
		reasons = append(reasons, fmt.Sprintf("%d new sector problem%s in the last %d days",
			c.Worsening, plural(c.Worsening), int(WorsenWindow.Hours()/24)))
	}

	// Write endurance is a countdown rather than a state, so it can outrank a healthy verdict: a
	// drive with 6% of its rated writes left is a drive to replace even while it is reporting
	// nothing wrong.
	if c.WearPct != nil {
		switch {
		case *c.WearPct >= replaceNowWear:
			sev = sevNow
			reasons = append(reasons, wearReason(*c.WearPct))
		case *c.WearPct >= replaceSoonWear:
			sev = max(sev, sevSoon)
			reasons = append(reasons, wearReason(*c.WearPct))
		}
	}

	return bandNames[sev], reasons
}

func wearReason(pct float64) string {
	return fmt.Sprintf("%.0f%% of its rated write endurance used", pct)
}

// rationaleOr prefers the drive's own words, because they are more specific than the band name.
func rationaleOr(rationale, fallback string) string {
	if rationale == "" {
		return fallback
	}
	return rationale
}

func plural(n int64) string {
	if n == 1 {
		return ""
	}
	return "s"
}
