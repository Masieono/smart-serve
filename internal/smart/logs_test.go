package smart

import (
	"strings"
	"testing"
)

// The ATA fixture is a capture from a real Samsung 850 EVO (`smartctl -x --json`). Its log
// sections are verbatim; the attribute table is trimmed to the rows that matter, since the
// logs are what this file is about.
func TestParseLogsATA(t *testing.T) {
	got := ParseLogs(loadFixture(t, "ata-850-evo-logs.json"))

	if got.ErrorCount == nil || *got.ErrorCount != 0 {
		t.Errorf("ErrorCount = %v, want 0", got.ErrorCount)
	}
	if got.SelfTestCount == nil || *got.SelfTestCount != 0 {
		t.Errorf("SelfTestCount = %v, want 0", got.SelfTestCount)
	}
	if got.SelfTestSupported == nil || !*got.SelfTestSupported {
		t.Errorf("SelfTestSupported = %v, want true", got.SelfTestSupported)
	}
	// The drive's own estimate of how long its tests run — the progress basis a running
	// test needs, and the reason this is parsed at all rather than left for A19.
	if got.ShortMinutes == nil || *got.ShortMinutes != 2 {
		t.Errorf("ShortMinutes = %v, want 2", got.ShortMinutes)
	}
	if got.ExtendedMinutes == nil || *got.ExtendedMinutes != 133 {
		t.Errorf("ExtendedMinutes = %v, want 133", got.ExtendedMinutes)
	}
	if got.Note != "" {
		t.Errorf("Note = %q, want empty on a drive with nothing to complain about", got.Note)
	}
}

// The BTO-1TB fixture is the only drive available to this project with a **populated** error
// log, and it is what settled the counts-only decision. On a drive smartctl does not recognise
// (`in_smartctl_database: false`), a logged error carries an error number and an index and
// nothing else — no LBA, no timestamp, no status — so a row per entry would be a row of dashes.
//
// Its self-test log is fully populated by contrast: type, status including the percentage
// remaining for a test in progress, and lifetime hours. That is the shape `A19` will render.
func TestParseLogsATAWithPopulatedLogs(t *testing.T) {
	got := ParseLogs(loadFixture(t, "ata-bto-1tb-logs.json"))

	if got.ErrorCount == nil || *got.ErrorCount != 1 {
		t.Errorf("ErrorCount = %v, want 1 — this drive has logged an error", got.ErrorCount)
	}
	if got.SelfTestCount == nil || *got.SelfTestCount != 2 {
		t.Errorf("SelfTestCount = %v, want 2", got.SelfTestCount)
	}
	if got.SelfTestSupported == nil || !*got.SelfTestSupported {
		t.Errorf("SelfTestSupported = %v, want true", got.SelfTestSupported)
	}
	if got.ShortMinutes == nil || *got.ShortMinutes != 2 {
		t.Errorf("ShortMinutes = %v, want 2", got.ShortMinutes)
	}
	if got.ExtendedMinutes == nil || *got.ExtendedMinutes != 10 {
		t.Errorf("ExtendedMinutes = %v, want 10", got.ExtendedMinutes)
	}
}

// The error log's single entry is the finding that justified waiting for a sample: a number and
// an index, with nothing else to show.
func TestParseLogsATAErrorEntries(t *testing.T) {
	got := ParseLogs(loadFixture(t, "ata-bto-1tb-logs.json"))

	if len(got.ErrorEntries) != 1 {
		t.Fatalf("ErrorEntries = %d, want 1", len(got.ErrorEntries))
	}
	e := got.ErrorEntries[0]
	if e.Number == nil || *e.Number != 1 {
		t.Errorf("Number = %v, want 1", e.Number)
	}
	if e.Index == nil || *e.Index != 0 {
		t.Errorf("Index = %v, want 0", e.Index)
	}
	if e.HasDetail() {
		t.Errorf("HasDetail = true for %+v; this drive reports nothing but a number", e)
	}
}

// The self-test log is the one worth rendering: it carries the test, its outcome, and the
// percentage remaining for a test still running — which is the progress reading A19 needs.
func TestParseLogsATASelfTestEntries(t *testing.T) {
	got := ParseLogs(loadFixture(t, "ata-bto-1tb-logs.json"))

	if len(got.SelfTestEntries) != 2 {
		t.Fatalf("SelfTestEntries = %d, want 2", len(got.SelfTestEntries))
	}

	done := got.SelfTestEntries[0]
	if done.Type != "Short offline" {
		t.Errorf("Type = %q, want %q", done.Type, "Short offline")
	}
	if done.Status != "Completed without error" {
		t.Errorf("Status = %q", done.Status)
	}
	if done.Passed == nil || !*done.Passed {
		t.Errorf("Passed = %v, want true", done.Passed)
	}
	if done.Remaining != nil {
		t.Errorf("Remaining = %v, want nil for a finished test", done.Remaining)
	}
	if done.Hours == nil || *done.Hours != 2116 {
		t.Errorf("Hours = %v, want 2116", done.Hours)
	}

	// A test in progress has a percentage and no verdict. An absent `passed` is not a failure,
	// which is why it stays nil rather than defaulting to false.
	running := got.SelfTestEntries[1]
	if running.Status != "Self-test routine in progress" {
		t.Errorf("Status = %q", running.Status)
	}
	if running.Remaining == nil || *running.Remaining != 50 {
		t.Errorf("Remaining = %v, want 50", running.Remaining)
	}
}

// An empty log must produce no entries rather than an empty table to render.
func TestParseLogsEmptyLogsProduceNoEntries(t *testing.T) {
	for _, fixture := range []string{"ata-850-evo-logs.json", "nvme-sn730-logs.json"} {
		got := ParseLogs(loadFixture(t, fixture))
		if len(got.ErrorEntries) != 0 || len(got.SelfTestEntries) != 0 {
			t.Errorf("%s: got %d error and %d self-test entries, want none",
				fixture, len(got.ErrorEntries), len(got.SelfTestEntries))
		}
	}
}

// The BTO-1TB fixture carries a test *in progress* alongside a finished one, which makes it the
// only place this rule can be checked against real data. It also pins the trap: the running test
// reports `passed: true`, so a rule built on `passed` would call it finished and hide it.
func TestRunningSelfTest(t *testing.T) {
	got := ParseLogs(loadFixture(t, "ata-bto-1tb-logs.json"))

	running := got.Running()
	if running == nil {
		t.Fatal("Running() = nil, but this drive has a test in progress")
	}
	if running.Remaining == nil || *running.Remaining != 50 {
		t.Errorf("Remaining = %v, want 50", running.Remaining)
	}
	if running.Status != "Self-test routine in progress" {
		t.Errorf("Status = %q", running.Status)
	}
	if running.Passed == nil || !*running.Passed {
		t.Fatalf("Passed = %v; this fixture exists to pin the trap that a running test passes",
			running.Passed)
	}

	for _, fixture := range []string{"ata-850-evo-logs.json", "nvme-sn730-logs.json"} {
		if r := ParseLogs(loadFixture(t, fixture)).Running(); r != nil {
			t.Errorf("%s: Running() = %+v, want nil", fixture, r)
		}
	}
}

// The NVMe fixture is a real WDC SN730. Two things about it matter:
//
//   - its SMART/Health log counts one error entry while the Error Information log emits no
//     entries at all, with or without `-l error` — so the count is the only thing available;
//   - it answers the self-test log request with an error, which says the *log* could not be read.
//     That is not the same finding as a drive that cannot test itself, and the same drive under a
//     newer smartctl reads the log fine — see `nvme-sn730-selftest.json`.
func TestParseLogsNVMe(t *testing.T) {
	got := ParseLogs(loadFixture(t, "nvme-sn730-logs.json"))

	if got.ErrorCount == nil || *got.ErrorCount != 1 {
		t.Errorf("ErrorCount = %v, want 1", got.ErrorCount)
	}
	if got.SelfTestSupported != nil {
		t.Errorf("SelfTestSupported = %v, want nil — this document says nothing about whether the "+
			"drive can test itself", got.SelfTestSupported)
	}
	if got.SelfTestCount != nil {
		t.Errorf("SelfTestCount = %v, want nil — the drive reported none", got.SelfTestCount)
	}
	if got.ShortMinutes != nil || got.ExtendedMinutes != nil {
		t.Errorf("polling minutes = %v/%v, want nil for NVMe", got.ShortMinutes, got.ExtendedMinutes)
	}
	if got.Note == "" {
		t.Error("Note is empty, so the unreadable self-test log would go unexplained")
	}
}

// A count is a number, not a constant: the fixtures happen to be 0 and 1, so prove a
// populated log is read rather than assumed.
func TestParseLogsReadsAPopulatedCount(t *testing.T) {
	raw := []byte(`{"ata_smart_error_log":{"extended":{"revision":1,"sectors":1,"count":7}}}`)
	got := ParseLogs(raw)
	if got.ErrorCount == nil || *got.ErrorCount != 7 {
		t.Errorf("ErrorCount = %v, want 7", got.ErrorCount)
	}
}

// Some drives answer with the shorter summary log, which may be reported as supported while
// being permanently empty. It is read when it is what the drive gave us.
func TestParseLogsFallsBackToTheSummaryLog(t *testing.T) {
	raw := []byte(`{"ata_smart_error_log":{"summary":{"revision":1,"count":3}}}`)
	got := ParseLogs(raw)
	if got.ErrorCount == nil || *got.ErrorCount != 3 {
		t.Errorf("ErrorCount = %v, want 3", got.ErrorCount)
	}
}

// A failed read leaves whatever smartctl managed to emit, which may be nothing, an error
// document, or a truncated fragment. None of those may panic or invent a summary.
func TestParseLogsToleratesRubbish(t *testing.T) {
	for _, raw := range [][]byte{nil, {}, []byte("not json"), []byte(`{"smartctl":{"messages":[]}}`)} {
		got := ParseLogs(raw)
		if got.ErrorCount != nil || got.SelfTestCount != nil || got.SelfTestSupported != nil {
			t.Errorf("ParseLogs(%q) = %+v, want an empty summary", raw, got)
		}
	}
}

// A self-test log the parser does not read must be *named* rather than silently dropped: the UI can
// then say that progress and past results are unavailable on this drive, which is a different
// statement from "no test has ever run" — and the only one of the two a reader can act on.
func TestParseLogsNamesASelfTestLogItCannotRead(t *testing.T) {
	got := ParseLogs([]byte(`{"vendor_self_test_log":{"table":[]}}`))

	if got.SelfTestLogUnread != "vendor_self_test_log" {
		t.Errorf("SelfTestLogUnread = %q, want vendor_self_test_log", got.SelfTestLogUnread)
	}
	// Empty would mean the API drops the whole summary and the UI never hears about it.
	if got.IsEmpty() {
		t.Error("a summary carrying an unread self-test log reports itself as empty")
	}
}

// The logs this parser *does* read must never be reported as unread, or a drive would claim its
// progress and results are unavailable while showing them.
func TestParseLogsDoesNotCallTheLogsItReadsUnread(t *testing.T) {
	for _, fixture := range []string{
		"ata-850-evo-logs.json", "ata-bto-1tb-logs.json", "nvme-sn730-selftest.json",
	} {
		if got := ParseLogs(loadFixture(t, fixture)); got.SelfTestLogUnread != "" {
			t.Errorf("%s: SelfTestLogUnread = %q, want empty", fixture, got.SelfTestLogUnread)
		}
	}
}

// The self-test log's *standard* variant, which is what a drive answers with when it does not keep
// the extended one. Reading only `extended` and `summary` meant such a drive reported no self-test
// log at all — no count, no entries, and no sign of the test it was running.
func TestParseLogsReadsTheStandardSelfTestLog(t *testing.T) {
	got := ParseLogs([]byte(`{
	  "ata_smart_self_test_log": {
	    "standard": {
	      "revision": 1,
	      "count": 2,
	      "table": [
	        {
	          "type": { "value": 1, "string": "Short offline" },
	          "status": { "value": 240, "string": "Self-test routine in progress",
	                      "remaining_percent": 40, "passed": true },
	          "lifetime_hours": 3505
	        },
	        {
	          "type": { "value": 1, "string": "Short offline" },
	          "status": { "value": 0, "string": "Completed without error", "passed": true },
	          "lifetime_hours": 3490
	        }
	      ]
	    }
	  }
	}`))

	if got.SelfTestCount == nil || *got.SelfTestCount != 2 {
		t.Errorf("SelfTestCount = %v, want 2", got.SelfTestCount)
	}

	running := got.Running()
	if running == nil {
		t.Fatal("Running() = nil, but the standard log has a test in progress")
	}
	if running.Remaining == nil || *running.Remaining != 40 {
		t.Errorf("Remaining = %v, want 40", running.Remaining)
	}

	done := got.LatestFinished()
	if done == nil || done.Status != "Completed without error" {
		t.Errorf("LatestFinished() = %+v", done)
	}
	if done.Hours == nil || *done.Hours != 3490 {
		t.Errorf("Hours = %v, want 3490", done.Hours)
	}
}

// The NVMe self-test log, captured from a real WDC SN730 under smartctl 7.5. This is the fixture
// that closed the gap `A19` had been carrying, and it is the whole reason the feature looked broken
// on NVMe: the test *in progress* is a header field here, not a table row, so a parser that only
// knew the ATA log reported "no test running" for a drive that was mid-test. The card, believing it,
// offered the run buttons again — and the drive refused the second one.
func TestParseLogsNVMeSelfTestLog(t *testing.T) {
	got := ParseLogs(loadFixture(t, "nvme-sn730-selftest.json"))

	if got.SelfTestSupported == nil || !*got.SelfTestSupported {
		t.Errorf("SelfTestSupported = %v, want true — the drive says it implements self-tests",
			got.SelfTestSupported)
	}
	if got.SelfTestLogUnread != "" {
		t.Errorf("SelfTestLogUnread = %q, want empty: this log is read", got.SelfTestLogUnread)
	}

	// The test in progress, with the drive's own progress figure turned into a percentage left.
	running := got.Running()
	if running == nil {
		t.Fatal("Running() = nil, but this drive has a short test in progress")
	}
	if running.Type != "Short" {
		t.Errorf("Type = %q, want Short", running.Type)
	}
	if running.Status != "Short self-test in progress" {
		t.Errorf("Status = %q, want the drive's own words", running.Status)
	}
	if running.Remaining == nil || *running.Remaining != 99 {
		t.Errorf("Remaining = %v, want 99 — the log reports 1%% complete", running.Remaining)
	}

	// The finished results, newest first, in the drive's own vocabulary.
	if got.SelfTestCount == nil || *got.SelfTestCount != 5 {
		t.Errorf("SelfTestCount = %v, want 5", got.SelfTestCount)
	}
	latest := got.LatestFinished()
	if latest == nil {
		t.Fatal("LatestFinished() = nil")
	}
	if latest.Type != "Extended" {
		t.Errorf("Type = %q, want Extended — the table is newest first", latest.Type)
	}
	if latest.Passed == nil || !*latest.Passed {
		t.Errorf("Passed = %v, want true", latest.Passed)
	}
	if latest.Hours == nil || *latest.Hours != 29650 {
		t.Errorf("Hours = %v, want 29650", latest.Hours)
	}
}

// A self-test log smartctl could not *read* is not the same finding as a drive that cannot test
// itself, and this project has the receipts: the fixture below is the same SN730 under smartctl 7.4,
// which reported "Read Self-test Log failed" — and under 7.5, with no hardware change, reads it
// fine. Concluding "this drive cannot run self-tests" from a failed read told the reader to give up
// on a drive that tests itself perfectly well.
func TestAnUnreadableLogIsNotAnUnsupportedDrive(t *testing.T) {
	got := ParseLogs(loadFixture(t, "nvme-sn730-logs.json"))

	if got.SelfTestSupported != nil {
		t.Errorf("SelfTestSupported = %v, want nil — nothing said the drive cannot test itself",
			got.SelfTestSupported)
	}
	if !strings.Contains(got.SelfTestLogUnread, "Read Self-test Log failed") {
		t.Errorf("SelfTestLogUnread = %q, want smartctl's own words", got.SelfTestLogUnread)
	}
}
