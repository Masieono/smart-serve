package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// CompareMaxDrives is how many drives one comparison may hold.
//
// Six, because a comparison is read *across* rows: beyond a handful of columns the reader is
// matching numbers by eye against a header they have scrolled away from, which is the thing the
// comparison exists to avoid. It is enforced here rather than in the UI because it is a resource
// limit as much as a readability one — the response carries a full attribute table per drive.
const CompareMaxDrives = 6

// compareDrive is one column of a comparison: who the drive is and how it is doing.
type compareDrive struct {
	Serial        string   `json:"serial"`
	Name          string   `json:"name"`
	Model         string   `json:"model"`
	ModelFamily   string   `json:"model_family,omitempty"`
	Bay           string   `json:"bay,omitempty"`
	Health        string   `json:"health"`
	Score         int      `json:"score"`
	TemperatureC  *int     `json:"temperature_c,omitempty"`
	CapacityBytes *uint64  `json:"capacity_bytes,omitempty"`
	PowerOnHours  *int64   `json:"power_on_hours,omitempty"`
	WearPct       *float64 `json:"wear_pct,omitempty"`
	Missing       bool     `json:"is_missing,omitempty"`
	// NoAttributes is true when the drive has never been read, or its read produced no attribute
	// table. The column is still shown — a drive that could not be read is a *result* of the
	// comparison rather than a reason to leave it out.
	NoAttributes bool `json:"no_attributes,omitempty"`
}

// compareCell is one drive's reading of one attribute.
//
// A cell is **absent** (a nil in the row's cell list) when the drive does not have the attribute at
// all, rather than present-with-zeros: an NVMe drive does not have "Reallocated Sector Count: 0", it
// does not have the attribute, and a zero there would be this app inventing a reading. That is why
// the row holds `[]*compareCell` and not a struct with a "present" flag — the absence is the shape.
type compareCell struct {
	Value     int    `json:"value"`
	Worst     int    `json:"worst"`
	Threshold int    `json:"threshold"`
	RawValue  uint64 `json:"raw_value"`
	RawString string `json:"raw_string,omitempty"`
	// Flagged is the drive's **own** verdict on this attribute: at or below the threshold it
	// publishes for itself (`A27`). The drive's alarm, not this app's opinion — the same rule the
	// device page and every export use, so a comparison cannot contradict the drive's own report.
	Flagged bool `json:"flagged,omitempty"`
}

// compareRow is one attribute, aligned across the drives.
type compareRow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Cells has one entry per drive, in the order the drives were asked for, so a cell's position is
	// its column. A nil is a drive that does not have this attribute.
	Cells []*compareCell `json:"cells"`
	// Differs is whether the present cells disagree about the value *or* the raw count. Both,
	// because which one carries the finding depends on the attribute: a healthy drive's normalised
	// value for reallocated sectors is 100 whatever the real count is, so comparing values alone
	// would miss the one difference a reader most wants to see.
	Differs bool `json:"differs,omitempty"`
	// Flagged is whether any drive has flagged this attribute, so a reader can see at a glance which
	// rows are worth reading.
	Flagged bool `json:"flagged,omitempty"`
}

type compareJSON struct {
	Drives []compareDrive `json:"drives"`
	Rows   []compareRow   `json:"rows"`
	// NVMe carries each drive's NVMe health log, nil for an ATA drive. Kept apart from the attribute
	// rows because it is a different shape entirely — counters rather than value/worst/threshold —
	// and an NVMe drive has no ATA attributes at all.
	NVMe []*model.NVMeHealth `json:"nvme"`
}

// handleCompare compares several drives' attribute tables, aligned by attribute id (`FEATURES.md`
// D6).
//
// Aligned here rather than in the browser, because the alignment *is* the feature: two drives of the
// same model report their attributes in whatever order smartctl felt like, and a table whose rows
// shuffle between drives is worse than no table. The union of ids, the ordering, and the "these
// disagree" marking are all decisions, and decisions live where they can be tested.
func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	serials, err := compareSerials(r.URL.Query().Get("serials"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	out := compareJSON{
		Drives: make([]compareDrive, 0, len(serials)),
		NVMe:   make([]*model.NVMeHealth, 0, len(serials)),
	}
	attributes := make([][]model.Attribute, 0, len(serials))

	for _, serial := range serials {
		device, derr := s.store.GetDevice(r.Context(), serial)
		if derr != nil {
			s.fail(w, "compare: get device", derr)
			return
		}
		if device == nil {
			writeError(w, http.StatusNotFound, "no such drive: "+serial)
			return
		}
		snap, serr := s.store.LatestSnapshot(r.Context(), serial)
		if serr != nil {
			s.fail(w, "compare: latest snapshot", serr)
			return
		}
		payload := decodeSnapshot(snap)

		out.Drives = append(out.Drives, compareDrive{
			Serial: device.Serial, Name: device.Name, Model: device.Model,
			ModelFamily: device.ModelFamily, Bay: device.Bay,
			Health: device.Health, Score: device.Score,
			TemperatureC: device.TemperatureC, CapacityBytes: device.CapacityBytes,
			PowerOnHours: snapshotHours(snap), WearPct: snapshotWear(snap),
			Missing:      device.IsMissing,
			NoAttributes: len(payload.Attributes) == 0 && payload.NVMe == nil,
		})
		attributes = append(attributes, payload.Attributes)
		out.NVMe = append(out.NVMe, payload.NVMe)
	}

	out.Rows = alignAttributes(attributes)
	writeJSON(w, http.StatusOK, out)
}

// snapshotHours and snapshotWear read the two figures that live on a snapshot rather than on the
// device row, tolerating a drive that has never been scanned.
func snapshotHours(snap *store.Snapshot) *int64 {
	if snap == nil {
		return nil
	}
	return snap.PowerOnHours
}

func snapshotWear(snap *store.Snapshot) *float64 {
	if snap == nil {
		return nil
	}
	return snap.WearPct
}

// compareSerials parses the request's serial list.
//
// Duplicates are dropped rather than refused: comparing a drive with itself is a mistake worth
// absorbing, and a comparison is a *view* rather than something to be strict about. The order the
// caller gave is kept, because that is the order the columns appear in.
func compareSerials(raw string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		serial := strings.TrimSpace(part)
		if serial == "" || seen[serial] {
			continue
		}
		seen[serial] = true
		out = append(out, serial)
	}
	switch {
	case len(out) == 0:
		return nil, fmt.Errorf("name the drives to compare, as ?serials=a,b,c")
	case len(out) == 1:
		return nil, fmt.Errorf("a comparison needs at least two drives")
	case len(out) > CompareMaxDrives:
		return nil, fmt.Errorf("at most %d drives at once", CompareMaxDrives)
	}
	return out, nil
}

// NVMe carries each drive's NVMe health log, nil for an ATA drive. Kept apart from the attribute
// rows because it is a different shape entirely — counters rather than value/worst/threshold —
// and an NVMe drive has no ATA attributes at all.

// alignAttributes turns several drives' attribute lists into rows keyed on attribute id.
//
// One cell per drive, in the order the drives were asked for, so a cell's position is its column. A
// drive missing an attribute gets an empty cell rather than a zero: an NVMe drive does not have
// "Reallocated Sector Count: 0", it does not have the attribute at all, and a zero there would be
// this app inventing a reading.
//
// The row's name comes from the first drive that named it. Two drives can disagree about an
// attribute's name — vendors rename them — and showing one name with the disagreement marked is more
// useful than two rows for one id.
func alignAttributes(perDrive [][]model.Attribute) []compareRow {
	order := []int{}
	byID := map[int]*compareRow{}

	for column, attributes := range perDrive {
		for _, a := range attributes {
			row, ok := byID[a.ID]
			if !ok {
				row = &compareRow{ID: a.ID, Name: a.Name, Cells: make([]*compareCell, len(perDrive))}
				byID[a.ID] = row
				order = append(order, a.ID)
			}
			if row.Name == "" {
				row.Name = a.Name
			}
			cell := &compareCell{
				Value:     a.Value,
				Worst:     a.Worst,
				Threshold: a.Threshold,
				RawValue:  a.RawValue,
				RawString: a.RawString,
				Flagged:   attributeFlagged(a),
			}
			row.Cells[column] = cell
			if cell.Flagged {
				row.Flagged = true
			}
		}
	}

	// Ordered by attribute id, which is the one order every drive agrees on: it is the number
	// CrystalDiskInfo shows, it is what the documentation calls each attribute by, and it does not
	// move when a drive is replaced or a vendor renames something.
	sort.Ints(order)

	out := make([]compareRow, 0, len(order))
	for _, id := range order {
		row := byID[id]
		row.Differs = cellsDiffer(row.Cells)
		out = append(out, *row)
	}
	return out
}

// cellsDiffer reports whether the drives disagree about an attribute.
//
// Only *present* cells are compared. A drive that does not have the attribute is not disagreeing
// about it, and counting absence as a difference would mark every row of a mixed ATA/NVMe comparison
// as interesting — which is the same as marking none of them.
//
// Both the normalised value and the raw count are compared, because which one carries the finding
// depends on the attribute: a healthy drive reports 100 for reallocated sectors whatever the real
// count is, so the value alone would call two drives with 0 and 12 bad sectors identical.
func cellsDiffer(cells []*compareCell) bool {
	var first *compareCell
	for _, cell := range cells {
		if cell == nil {
			continue
		}
		if first == nil {
			first = cell
			continue
		}
		if cell.Value != first.Value || cell.RawValue != first.RawValue {
			return true
		}
	}
	return false
}

// attributeFlagged is the drive's own alarm condition: the attribute is at or below the threshold
// the drive publishes for itself.
//
// The same rule as the device page and the exports (`A27`). A threshold of zero means *no
// threshold*, not *fails at zero* — which is the mistake that would flag every attribute on a drive
// that publishes zeros, and is why this is a named function rather than an inline comparison.
func attributeFlagged(a model.Attribute) bool {
	return a.Threshold > 0 && a.Value <= a.Threshold
}
