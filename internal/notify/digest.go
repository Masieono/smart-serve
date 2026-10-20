package notify

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/store"
)

// digestListLimit caps each section of a digest.
//
// A digest is one message, and a section listing twenty-four drives is not a summary — it is the
// dashboard in a notification tray. The cap is on *entries*, and what was left out is always said,
// because a silently truncated list reads as a complete one.
const digestListLimit = 5

// Digest is one period's summary (`FEATURES.md` B13).
//
// Plain data rather than a store handle, so the wording is a pure function of the facts and can be
// tested without a database. Everything in it is something the app already computes for the
// dashboard — the snapshots *are* the history, so a digest needs no storage of its own.
type Digest struct {
	From, To time.Time
	// Devices is the fleet as it stands now, in the store's order (worst verdict first).
	Devices []store.DeviceRecord
	// Facts is the newest snapshot's figures per drive, for the wear half of "needs attention".
	Facts map[string]store.SnapshotFacts
	// Before is each drive's verdict at the start of the period, from `HealthAt`. A drive absent
	// from it was not being watched yet, which is not a change.
	Before map[string]string
	// Worsening is bad-sector growth over the period, per drive.
	Worsening map[string]int64
	// ScansOK and ScansFailed are the period's scans, so the digest can explain a gap in history.
	ScansOK, ScansFailed int
	// Ranking is the replace-first shortlist (`A24`), already in the server's order.
	Ranking []interp.ReplacementRank
}

// DigestEvent turns a period's summary into one notification (`FEATURES.md` B13).
//
// **One message instead of a stream.** The event catalog is exception-shaped: every other event fires
// because something changed, and a quiet week sends nothing at all. That is right for alerts and
// wrong for the question "is my fleet okay?", which a weekly digest answers in a sentence.
//
// The priority reflects the content rather than the event type — a digest of a healthy fleet is a
// note, and a digest naming a failing drive is not — because a weekly message that always arrives at
// the same importance stops carrying information.
//
// It is **not** opt-in the way `scan_success` and `group_heat` are: those two would be sent to
// channels that never asked, whereas a digest only exists because somebody created a digest schedule.
// The schedule *is* the subscription, so a channel subscribed to everything should receive it.
func DigestEvent(d Digest, opts CollectOptions) Event {
	ev := Event{
		Type:     EventDigest,
		Priority: PriorityLow,
		ScanTS:   d.To.UnixMilli(),
		Version:  opts.Version,
		// One key per fire, so squelching never applies: a digest is a *scheduled* message, and
		// suppressing a weekly summary because a previous one arrived an hour ago would mean the
		// schedule and the notification disagreed about whether it ran.
		key:      EventDigest + ":" + d.To.UTC().Format("2006-01-02T15:04"),
		baseURL:  opts.BaseURL,
		tempUnit: opts.TemperatureUnit,
	}
	ev.Rationale = digestSummary(d)

	counts := map[string]int{}
	for _, dev := range d.Devices {
		counts[dev.Health]++
	}
	if counts["bad"] > 0 || counts["caution"] > 0 {
		ev.Priority = PriorityMedium
	}
	return ev
}

// digestSummary is the digest's text: the fleet as it stands, what moved, and what to do about it.
//
// Sections are omitted when they have nothing in them, the same rule as the evidence blocks in a
// report: a heading with "none" under it is noise, and a reader learns to skip the whole message.
func digestSummary(d Digest) string {
	var lines []string

	counts := map[string]int{}
	for _, dev := range d.Devices {
		counts[dev.Health]++
	}
	lines = append(lines, fmt.Sprintf("%d drives: %s", len(d.Devices), verdictCounts(counts)))

	if changes := digestChanges(d); len(changes) > 0 {
		lines = append(lines, "Changed: "+strings.Join(changes, ", "))
	}
	if worse := digestWorsening(d); len(worse) > 0 {
		lines = append(lines, "Worse: "+strings.Join(worse, ", "))
	}
	// Always said, including the zero: "7 completed, 0 failed" is the reassurance a digest exists to
	// give, and an absent line would be ambiguous between "none failed" and "we did not look".
	lines = append(lines, fmt.Sprintf("Scans: %d completed, %d failed", d.ScansOK, d.ScansFailed))

	if replace := digestReplaceFirst(d); len(replace) > 0 {
		lines = append(lines, "Replace first: "+strings.Join(replace, ", "))
	}
	return strings.Join(lines, "\n")
}

// verdictCounts renders the fleet's verdicts worst-first, naming only the ones that are there.
func verdictCounts(counts map[string]int) string {
	labels := []struct{ key, word string }{
		{"bad", "failing"},
		{"caution", "caution"},
		{"unknown", "unknown"},
		{"good", "healthy"},
	}
	var parts []string
	for _, l := range labels {
		if n := counts[l.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, l.word))
		}
	}
	if len(parts) == 0 {
		return "none recorded yet"
	}
	return strings.Join(parts, " · ")
}

// digestChanges lists the drives whose verdict moved over the period.
//
// Only drives with a baseline are considered: a drive that was not being watched at the start of the
// period has not changed, it has arrived, and reporting it as a change would make every first week
// look like a fleet-wide event.
func digestChanges(d Digest) []string {
	var out []string
	for _, dev := range d.Devices {
		was, known := d.Before[dev.Serial]
		if !known || was == "" || was == dev.Health {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s → %s", deviceLabel(dev), was, dev.Health))
	}
	sort.Strings(out)
	return capList(out, "changed")
}

// digestWorsening lists the drives that lost storage over the period, worst first: the drive that
// lost the most is the one to read about.
func digestWorsening(d Digest) []string {
	type entry struct {
		serial string
		delta  int64
		label  string
	}
	var entries []entry
	for _, dev := range d.Devices {
		if delta := d.Worsening[dev.Serial]; delta > 0 {
			entries = append(entries, entry{dev.Serial, delta, deviceLabel(dev)})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].delta != entries[j].delta {
			return entries[i].delta > entries[j].delta
		}
		return entries[i].serial < entries[j].serial
	})

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, fmt.Sprintf("%s +%d sectors", e.label, e.delta))
	}
	return capList(out, "worse")
}

// digestReplaceFirst names the drives the ranking says to act on, in its own words — the same words
// the dashboard shows, so the two cannot disagree about which drive to buy.
func digestReplaceFirst(d Digest) []string {
	var out []string
	for _, r := range d.Ranking {
		if r.Band == interp.ReplaceFine {
			continue
		}
		name := r.Name
		if name == "" {
			name = r.Serial
		}
		reason := ""
		if len(r.Reasons) > 0 {
			reason = " (" + r.Reasons[0] + ")"
		}
		out = append(out, name+reason)
	}
	return capList(out, "replace")
}

// capList truncates a section and says how much it left out, because a truncated list that does not
// say so reads as a complete one.
func capList(items []string, what string) []string {
	if len(items) <= digestListLimit {
		return items
	}
	kept := items[:digestListLimit]
	return append(kept, fmt.Sprintf("and %d more %s", len(items)-digestListLimit, what))
}

// deviceLabel names a drive as concisely as the record allows, preferring what a person assigned.
func deviceLabel(d store.DeviceRecord) string {
	switch {
	case d.Name != "":
		return d.Name
	case d.Model != "":
		return d.Model
	default:
		return d.Serial
	}
}
