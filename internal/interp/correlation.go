package interp

import (
	"fmt"
	"sort"
)

// CorrelationThresholds are the two tunables for correlated-change detection (`FEATURES.md` A25).
//
// Settings rather than constants, because the fleet size this has to suit spans an order of
// magnitude: four drives in a workstation and twenty-four in a NAS want different answers, and a
// fixed number would be wrong for one of them (`ADR-023`).
type CorrelationThresholds struct {
	// MinDrives is how many drives in one group have to move before the group is a finding.
	MinDrives int
	// DeltaC is how far a drive's temperature has to rise to count as having moved.
	DeltaC int
}

// DefaultCorrelationThresholds are chosen for a small enclosure (`ADR-023`).
//
// Four drives is a fraction of a workstation's fleet and a sixth of a 24-bay NAS, so a bigger
// enclosure should raise MinDrives: the question the rule asks is "did enough of this group move at
// once to be one cause rather than several", and "enough" grows with the group. Eight degrees is
// above the day-to-day wander of a drive at idle and below the rise a stopped fan produces.
func DefaultCorrelationThresholds() CorrelationThresholds {
	return CorrelationThresholds{MinDrives: 4, DeltaC: 8}
}

// HeatReading is one drive's temperature now and at the previous reading.
type HeatReading struct {
	Serial string
	// Groups are the tags the drive belongs to, already split by the caller. Which tags exist is
	// the store's business, so this stays a list of names rather than a string to parse — the
	// interpreter has no opinion about how tags are stored.
	Groups []string
	// Prev and Now are nil when the drive reported no temperature at that reading. A drive with
	// neither is not "unchanged", it is unmeasured, and it is never counted as having moved.
	Prev *int
	Now  *int
}

// GroupHeat is one group whose drives moved together (`FEATURES.md` A25).
type GroupHeat struct {
	Group string `json:"group"`
	// Drives are the serials that moved, and Total is the size of the group they moved within. Both
	// are carried because the ratio is the reader's judgement to make, not the app's: six of six is
	// a different finding from six of twenty-four.
	Drives []string `json:"drives"`
	Total  int      `json:"total"`
	// DeltaC is the *smallest* rise among the drives that moved, so the figure is true of every
	// drive named. A mean would let one drive's spike stand in for the others.
	DeltaC int `json:"delta_c"`
	// MaxC is the hottest current reading in the group, nil when none reported one.
	MaxC *int `json:"max_c,omitempty"`
}

// CorrelatedHeat finds the groups whose drives moved together (`FEATURES.md` A25).
//
// The insight is that an enclosure is shared: eight drives in one chassis have one fan, one intake
// and one ambient, so a dying fan presents as *all eight* running hotter at once. Per-drive
// monitoring reports that as eight unrelated alerts, which is precisely the noise that teaches
// people to ignore alerts. This reports it once, naming the group.
//
// **It reports a correlation, not a cause, and the wording has to say so.** A RAID rebuild also
// heats everything, and so does a warm afternoon; nothing observable from a temperature alone tells
// them apart. So the rule's job is to notice that *several drives moved at once*, which is
// information the per-drive view does not carry, and then to leave the diagnosis to the reader, who
// knows whether a rebuild is running. A false "your enclosure is overheating" is worse than eight
// true per-drive alerts (`ADR-023`), and the way to avoid one is not to make the claim.
//
// A drive counts for each of its tags, the same rule as the grouping (`D9`): a group that hid a
// drive would be worse than one that reports it twice.
//
// Ordered by how many drives moved, then by the shared rise, then by name, so the list is stable
// between renders.
func CorrelatedHeat(readings []HeatReading, t CorrelationThresholds) []GroupHeat {
	if t.MinDrives < 2 || t.DeltaC <= 0 {
		return nil
	}

	type acc struct {
		moved  []string
		total  int
		delta  int // the smallest rise among the movers so far, -1 for "none yet"
		maxC   int
		hasMax bool
	}
	groups := map[string]*acc{}

	for _, r := range readings {
		// A reading with no current temperature is not a drive that held steady, it is a drive that
		// did not report — so it is not in the denominator either. The input contract is that `Now`
		// is set; a caller that has unmeasured drives leaves them out.
		if r.Now == nil {
			continue
		}
		rise, moved := 0, false
		if r.Prev != nil {
			rise = *r.Now - *r.Prev
			moved = rise >= t.DeltaC
		}
		for _, tag := range r.Groups {
			g, ok := groups[tag]
			if !ok {
				g = &acc{delta: -1}
				groups[tag] = g
			}
			g.total++
			if !g.hasMax || *r.Now > g.maxC {
				g.maxC, g.hasMax = *r.Now, true
			}
			if !moved {
				continue
			}
			g.moved = append(g.moved, r.Serial)
			if g.delta < 0 || rise < g.delta {
				g.delta = rise
			}
		}
	}

	var out []GroupHeat
	for name, g := range groups {
		if len(g.moved) < t.MinDrives {
			continue
		}
		gh := GroupHeat{Group: name, Drives: g.moved, Total: g.total, DeltaC: g.delta}
		if g.hasMax {
			max := g.maxC
			gh.MaxC = &max
		}
		sort.Strings(gh.Drives)
		out = append(out, gh)
	}

	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Drives) != len(out[j].Drives) {
			return len(out[i].Drives) > len(out[j].Drives)
		}
		if out[i].DeltaC != out[j].DeltaC {
			return out[i].DeltaC > out[j].DeltaC
		}
		return out[i].Group < out[j].Group
	})
	return out
}

// HeatSummary is one group's finding in a sentence, for a notification body or a card.
//
// `delta` is the already-formatted rise, so the unit handling stays in the one place that owns it.
//
// Deliberately an observation with the likely places to look, and never a diagnosis: the reader
// knows whether a rebuild is running and the app does not.
func HeatSummary(g GroupHeat, delta string) string {
	where := "they share an enclosure"
	if len(g.Drives) < g.Total {
		where = "most of them share an enclosure"
	}
	return fmt.Sprintf("%d of the %d drives tagged %s are at least %s hotter than at the previous "+
		"reading. %s, so the cause is more likely to be there — a fan, a filter, or ambient — than in "+
		"%d drives at once.", len(g.Drives), g.Total, g.Group, delta, where, len(g.Drives))
}

// CorrelationFromSettings reads the two tunables out of the settings map (`ADR-023`).
//
// A missing or unparseable value falls back to the documented default rather than to zero, and the
// guards say why: `MinDrives` below two is not "off", it is "any two drives that moved", and a
// `DeltaC` of zero or less would call every drive that held steady a mover. Neither is what an unset
// setting means, and both would be a way for a typo to produce a wrong alert.
func CorrelationFromSettings(settings map[string]string) CorrelationThresholds {
	t := DefaultCorrelationThresholds()
	if v, ok := settingInt(settings, "correlation_min_drives"); ok && v >= 2 {
		t.MinDrives = v
	}
	if v, ok := settingInt(settings, "correlation_delta_c"); ok && v > 0 {
		t.DeltaC = v
	}
	return t
}
