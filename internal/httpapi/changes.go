package httpapi

import (
	"encoding/json"
	"sort"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// changeJSON is one field that moved between two scans.
//
// Both ends are `any` because a field can be absent at either end, and absent is not zero: a drive
// that reported no temperature last scan has no `from`, and rendering one as 0 °C would invent a
// reading the drive never gave.
type changeJSON struct {
	From any `json:"from,omitempty"`
	To   any `json:"to,omitempty"`
}

// attributeChangeJSON is one attribute whose value moved.
type attributeChangeJSON struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	From      uint64 `json:"from"`
	To        uint64 `json:"to"`
	Value     int    `json:"value"`
	Worst     int    `json:"worst"`
	Threshold int    `json:"threshold"`
	// Trouble marks a change the app already treats as a finding, and the two rules are ones it
	// already uses: the drive's own threshold is met, or the attribute is one of the lost-storage
	// counters. Nothing new is invented here, which is what stops the diff from disagreeing with
	// the verdict about the same drive.
	Trouble bool `json:"trouble,omitempty"`
	// Appeared marks an attribute that was not in the previous scan at all. A counter that has
	// just started being reported is news whatever its value.
	Appeared bool `json:"appeared,omitempty"`
}

// changesJSON is what changed since the last scan (`FEATURES.md` A18).
type changesJSON struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`

	Health       *changeJSON `json:"health,omitempty"`
	Score        *changeJSON `json:"score,omitempty"`
	TemperatureC *changeJSON `json:"temperature_c,omitempty"`
	WearPct      *changeJSON `json:"wear_pct,omitempty"`
	Sectors      *changeJSON `json:"sectors,omitempty"`

	// Attributes are the counters that moved *and* that the app considers a finding, worst first.
	Attributes []attributeChangeJSON `json:"attributes,omitempty"`
	// Routine counts the counters that moved and are not a finding — power-on hours, temperature,
	// load cycles. Reported rather than dropped, and deliberately: a silent filter would be a lie
	// by omission, while "4 other counters moved" is both honest and enough.
	Routine int `json:"routine,omitempty"`
	// Note explains an absent diff. An absent diff and an empty one are different findings, and
	// only the empty one means "nothing changed".
	Note string `json:"note,omitempty"`
}

// storedAttributes is the shape of the attributes blob a snapshot carries.
type storedAttributes struct {
	Attributes []model.Attribute `json:"attributes"`
	NVMe       *model.NVMeHealth `json:"nvme,omitempty"`
}

// diffSnapshots compares two snapshots of the same drive.
//
// `prev` may be nil, which is the first-scan case: there is nothing to compare against, and saying
// so beats rendering a page of zero changes that would read as "nothing happened".
func diffSnapshots(prev, cur *store.Snapshot) *changesJSON {
	if cur == nil {
		return nil
	}
	out := &changesJSON{To: rfc3339(cur.TS)}
	if prev == nil {
		out.Note = "Only one scan has been recorded for this drive, so there is nothing to compare " +
			"against yet. A diff appears after the second scan."
		return out
	}
	out.From = rfc3339(prev.TS)

	out.Health = diffString(prev.Health, cur.Health)
	out.Score = diffInt(prev.Score, cur.Score)
	out.TemperatureC = diffIntPtr(prev.TemperatureC, cur.TemperatureC)
	out.WearPct = diffFloatPtr(prev.WearPct, cur.WearPct)
	out.Sectors = diffInt64Ptr(prev.Sectors, cur.Sectors)

	out.Attributes, out.Routine = diffAttributes(prev.Attributes, cur.Attributes)
	return out
}

func diffString(from, to string) *changeJSON {
	if from == to {
		return nil
	}
	return &changeJSON{From: from, To: to}
}

func diffInt(from, to int) *changeJSON {
	if from == to {
		return nil
	}
	return &changeJSON{From: from, To: to}
}

func diffIntPtr(from, to *int) *changeJSON {
	if intPtrEqual(from, to) {
		return nil
	}
	return &changeJSON{From: intValue(from), To: intValue(to)}
}

func diffInt64Ptr(from, to *int64) *changeJSON {
	if int64PtrEqual(from, to) {
		return nil
	}
	return &changeJSON{From: int64Value(from), To: int64Value(to)}
}

func diffFloatPtr(from, to *float64) *changeJSON {
	if floatPtrEqual(from, to) {
		return nil
	}
	return &changeJSON{From: floatValue(from), To: floatValue(to)}
}

// The *Value helpers dereference a reading, or return nil when the drive reported none.
//
// Nil rather than zero, because absent and zero are different findings — and a typed nil pointer
// inside an `any` is not "empty" to `omitempty`, so passing the pointer straight through would emit
// a `null` that reads as a value the drive gave.
func intValue(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func int64Value(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func floatValue(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// A nil pointer means "the drive did not report this", which is a different finding from any value
// — so nil equals only nil, and never zero.
func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func int64PtrEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// diffAttributes splits the attributes that moved into the ones the app treats as a finding and the
// ones that are routine wear.
//
// Nothing new is invented: `trouble` is the drive's own threshold being met, or the attribute being
// one of the lost-storage counters. A second opinion here would let the diff and the verdict
// disagree about the same drive, which is the one thing an attribute-level judgement must not do.
func diffAttributes(prevJSON, curJSON string) (trouble []attributeChangeJSON, routine int) {
	before := attributesByID(prevJSON)

	for _, a := range decodeAttributes(curJSON) {
		previous, existed := before[a.ID]
		if existed && previous.RawValue == a.RawValue && previous.Value == a.Value {
			continue
		}
		entry := attributeChangeJSON{
			ID: a.ID, Name: a.Name, To: a.RawValue,
			Value: a.Value, Worst: a.Worst, Threshold: a.Threshold,
			Appeared: !existed,
		}
		if existed {
			entry.From = previous.RawValue
		}
		if interp.IsSectorAttribute(a.ID) || flaggedAttribute(a) {
			entry.Trouble = true
			trouble = append(trouble, entry)
			continue
		}
		routine++
	}

	// Worst first: a counter that moved and is at its threshold matters more than one that is not.
	// Ties break on the ID so the order is stable between renders.
	sort.SliceStable(trouble, func(i, j int) bool {
		if trouble[i].Value != trouble[j].Value {
			return trouble[i].Value < trouble[j].Value
		}
		return trouble[i].ID < trouble[j].ID
	})
	return trouble, routine
}

// flaggedAttribute is the drive's own threshold being met, which is the rule the attribute table
// already uses. A threshold of 0 means the attribute has none, not that it fails at zero.
func flaggedAttribute(a model.Attribute) bool {
	return a.Threshold > 0 && a.Value <= a.Threshold
}

func decodeAttributes(raw string) []model.Attribute {
	if raw == "" {
		return nil
	}
	var stored storedAttributes
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		// A blob we cannot read is not worth failing a page for: the diff simply has no attribute
		// changes to report, and the rest of it is still true.
		return nil
	}
	return stored.Attributes
}

func attributesByID(raw string) map[int]model.Attribute {
	out := map[int]model.Attribute{}
	for _, a := range decodeAttributes(raw) {
		out[a.ID] = a
	}
	return out
}
