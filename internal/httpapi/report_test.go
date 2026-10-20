package httpapi

import (
	"slices"
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// A27 is one paragraph plus the evidence. The paragraph is the part that makes the file usable by
// someone who did not generate it — a support engineer, or the reader of a forum post.
func TestEveryReportSaysWhatItIsFor(t *testing.T) {
	d := store.DeviceRecord{Serial: "S1", Model: "M", Health: "good"}
	snap := &store.Snapshot{}

	for _, tc := range []struct{ format, body string }{
		{"txt", renderTextReport(d, snap)},
		{"html", renderHTMLReport(d, snap)},
		{"csv", renderCSVReport(d, snap)},
	} {
		if !strings.Contains(tc.body, "Warranty / RMA evidence") {
			t.Errorf("%s report does not say what it is for:\n%s", tc.format, tc.body)
		}
	}
}

// The drive's *own* threshold, not this app's. An RMA turns on "attribute 5 is at or below its own
// threshold" — a limit the vendor wrote into the drive — and on a listing rather than a verdict, so
// a threshold the user changed since the scan cannot make the report disagree with itself.
func TestTheReportNamesTheFailingAttributes(t *testing.T) {
	snap := &store.Snapshot{Attributes: `{"attributes":[` +
		`{"id":5,"name":"Reallocated_Sector_Ct","value":1,"worst":1,"threshold":36,"raw_value":5},` +
		`{"id":9,"name":"Power_On_Hours","value":99,"worst":99,"threshold":0,"raw_value":1234},` +
		`{"id":197,"name":"Current_Pending_Sector","value":100,"worst":100,"threshold":0,"raw_value":0},` +
		`{"id":199,"name":"UDMA_CRC_Error_Count","value":200,"worst":200,"raw_value":3}]}`}
	d := store.DeviceRecord{Serial: "S1", Model: "M", Health: "bad", HealthRationale: "5 reallocated sectors"}

	body := renderTextReport(d, snap)
	if !strings.Contains(body, "Failing attributes") || !strings.Contains(body, "Reallocated_Sector_Ct") {
		t.Errorf("the failing attribute is not named in:\n%s", body)
	}
	// The comparison, not only the conclusion: an RMA needs the numbers, and the drive's own
	// threshold is the one the argument is about.
	if !strings.Contains(body, "threshold  36") {
		t.Errorf("the drive's own threshold is missing from:\n%s", body)
	}

	// The counters section is about the number rather than the comparison. Attribute 5 is in both
	// sections because it is two facts about it, but printing the six-column line twice would say it
	// had been duplicated — and attribute 199, which has no threshold failure, is only here.
	i := strings.Index(body, "Evidence counters")
	if i < 0 {
		t.Fatalf("no counters section in:\n%s", body)
	}
	counters := body[i:]
	if !strings.Contains(counters, "UDMA_CRC_Error_Count") {
		t.Errorf("a non-zero counter is missing from:\n%s", counters)
	}
	if strings.Contains(counters, "threshold") {
		t.Errorf("the counters section repeats the threshold comparison:\n%s", counters)
	}

	// A threshold of zero means "no threshold", not "must be zero": plenty of attributes report 0
	// there, and listing every one of them would bury the real findings in noise.
	failed := failingAttributes([]model.Attribute{
		{ID: 9, Value: 99, Threshold: 0},
		{ID: 197, Value: 100, Threshold: 0},
	})
	if len(failed) != 0 {
		t.Errorf("failing = %+v, want none — a zero threshold is not a failure", failed)
	}
}

// A section with nothing in it is omitted. "Nothing here" and "we did not look" are different
// findings, and a table of zeros reads as a clean bill of health the app has not issued.
func TestACleanDriveGetsNoEvidenceSections(t *testing.T) {
	snap := &store.Snapshot{Attributes: `{"attributes":[` +
		`{"id":5,"name":"Reallocated_Sector_Ct","value":100,"worst":100,"threshold":36,"raw_value":0},` +
		`{"id":197,"name":"Current_Pending_Sector","value":100,"worst":100,"raw_value":0}]}`}
	d := store.DeviceRecord{Serial: "S1", Model: "M", Health: "good"}

	for _, tc := range []struct{ format, body string }{
		{"txt", renderTextReport(d, snap)},
		{"html", renderHTMLReport(d, snap)},
	} {
		for _, unwanted := range []string{"Failing attributes", "Evidence counters"} {
			if strings.Contains(tc.body, unwanted) {
				t.Errorf("%s report has a %q section for a clean drive:\n%s", tc.format, unwanted, tc.body)
			}
		}
	}
}

// The ids come from the interpreter, so a report cannot name a different set of attributes than the
// verdict reads — and it reads their counts the interpreter's way, so it cannot print 0 for a drive
// the verdict called failing.
func TestTheEvidenceCountersAreTheInterpretersOwn(t *testing.T) {
	var ids []int
	for _, ev := range interp.EvidenceAttributes() {
		ids = append(ids, ev.ID)
	}
	for _, want := range []int{5, 187, 196, 197, 198, 199} {
		if !slices.Contains(ids, want) {
			t.Errorf("attribute %d is no longer read as evidence", want)
		}
	}

	// Attribute 199 is not one of the interpreter's "lost storage" ids, but it is evidence, and the
	// report has to list it for the same reason the verdict reads it.
	counters := evidenceCounters([]model.Attribute{{ID: 199, Name: "UDMA_CRC_Error_Count", RawValue: 3}})
	if len(counters) != 1 || counters[0].ID != 199 {
		t.Errorf("counters = %+v, want attribute 199", counters)
	}

	// The packed-word rule is the interpreter's (`SMART_INTERPRETATION.md` §3), applied here by the
	// interpreter, so the two cannot read the same raw value differently.
	packed := evidenceCounters([]model.Attribute{{ID: 5, Name: "Reallocated_Sector_Ct", RawValue: 1 << 16}})
	if len(packed) != 1 {
		t.Fatalf("packed counter was dropped: %+v", packed)
	}
	if got := interp.AttributeCount(packed[0]); got != 1 {
		t.Errorf("packed count = %d, want 1", got)
	}
}
