package smart

import (
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

// The NVMe fixture is a real WDC SN730. Two things about it matter:
//
//   - its SMART/Health log counts one error entry while the Error Information log emits no
//     entries at all, with or without `-l error` — so the count is the only thing available;
//   - it answers the self-test log request with an error, which is the sole signal that the
//     drive has no self-test log. Nothing else in the document reports it.
func TestParseLogsNVMe(t *testing.T) {
	got := ParseLogs(loadFixture(t, "nvme-sn730-logs.json"))

	if got.ErrorCount == nil || *got.ErrorCount != 1 {
		t.Errorf("ErrorCount = %v, want 1", got.ErrorCount)
	}
	if got.SelfTestSupported == nil || *got.SelfTestSupported {
		t.Errorf("SelfTestSupported = %v, want false", got.SelfTestSupported)
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
