package interp

import "github.com/masieono/smart-serve/internal/model"

// EvidenceAttribute is one ATA attribute this app reads as evidence of a problem.
type EvidenceAttribute struct {
	ID    int
	Label string
}

// evidenceAttributes are the attributes the verdict reads, with the name to print for each.
//
// Exported because the report export (`FEATURES.md` A27) has to list them, and it has to list
// *these*: an RMA is an argument about specific counters, and an argument citing a number the app
// does not otherwise look at is one the app cannot stand behind. Keeping one list means a report
// and a verdict can never name different attributes for the same drive.
//
// The ids here are the ones `evaluateATA` reads — 5, 197 and 198 as lost storage; 187, 196 and 199
// as recorded errors; 10 as a mechanical fault. A new check there belongs here too.
//
// This is the evidence, not the verdict. Whether a count is *enough* to call a drive bad is a
// threshold the user sets, and the report says nothing about it: a non-zero reallocated sector
// count is a fact, and "one sector is fine" is an opinion.
var evidenceAttributes = []EvidenceAttribute{
	{ID: 5, Label: "Reallocated sectors"},
	{ID: 10, Label: "Spin retries"},
	{ID: 187, Label: "Reported uncorrectable errors"},
	{ID: 196, Label: "Reallocation events"},
	{ID: 197, Label: "Current pending sectors"},
	{ID: 198, Label: "Offline uncorrectable sectors"},
	{ID: 199, Label: "UDMA CRC errors"},
}

// EvidenceAttributes returns the attributes this app reads as evidence, in a fixed order.
//
// A copy, so a caller cannot reorder or truncate the list for everyone else.
func EvidenceAttributes() []EvidenceAttribute {
	out := make([]EvidenceAttribute, len(evidenceAttributes))
	copy(out, evidenceAttributes)
	return out
}

// AttributeCount reads an attribute's raw count the way the verdict does: the packed-word rule
// (docs/SMART_INTERPRETATION.md §3) applied to the raw value.
//
// Exported so a report cannot print 0 for a drive the verdict called failing. Reading the raw value
// differently in the two places is exactly the drift this file exists to prevent, so this is the
// interpreter's own reading rather than a second implementation of it.
func AttributeCount(a model.Attribute) uint64 {
	return effectiveCount(a.RawValue)
}
