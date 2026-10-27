package smart

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/masieono/smart-serve/internal/model"
)

// ParseLogs reads a drive's log summaries out of a `smartctl --json` document.
//
// **Only the counts are modelled, and that is a deliberate limit rather than an oversight.**
// The counts and capabilities below are verified against real hardware on both transports
// (see the fixtures `ata-850-evo-logs.json` and `nvme-sn730-logs.json`). The per-entry field
// names are *not*, because neither drive available to this project has ever logged an error,
// and smartctl emits no `table` at all when a log is empty — so the populated shape has never
// been observed. Guessing it would produce a panel that renders nothing precisely when a
// drive has something to say, which is the one moment it matters.
//
// The entries are still reachable: the raw output is stored verbatim for every drive
// (`FEATURES.md` A10) and the panel points at it. When a populated log is captured, entry
// rendering can be added against something real.
//
// This parses on read rather than storing a parsed copy. A log summary describes the drive's
// *current* state rather than a point in a series, and the raw document it comes from is
// already stored.
func ParseLogs(raw []byte) model.LogSummary {
	var r rawLogDoc
	if len(raw) == 0 || json.Unmarshal(raw, &r) != nil {
		return model.LogSummary{}
	}

	var out model.LogSummary

	// A self-test log the drive has that this parser does not read. Reported rather than ignored:
	// the UI can then say that a test's progress and past results cannot be shown on this drive,
	// instead of leaving a panel that stays empty — which looks exactly like a test that never ran.
	out.SelfTestLogUnread = unreadSelfTestLog(raw)

	// The drive's own answer to "do you implement self-tests?", when it gives one. A direct
	// statement beats any inference drawn from whether a log could be read.
	if c := r.NVMeAdminCommands; c != nil && c.SelfTest != nil {
		out.SelfTestSupported = c.SelfTest
	}

	// The NVMe self-test log. A different shape from the ATA one in the way that mattered: the test
	// in progress is a *header* field rather than a table row, so a parser that only knew the ATA log
	// reported "no test running" for a drive that was mid-test — and the card, believing it, offered
	// the run buttons again and the drive refused the second one.
	if l := r.NVMeSelfTestLog; l != nil {
		// A readable self-test log is the drive telling us it can test itself.
		supported := true
		out.SelfTestSupported = &supported

		if op := l.CurrentSelfTestOperation; op != nil && op.Value != nil && *op.Value != 0 {
			// The progress figure is a header field here, so it is turned into the same
			// percentage-remaining the ATA log carries: `Running` keys on that field and nothing
			// else, which is what keeps one rule for both transports.
			remaining := 0
			if l.CompletionPercent != nil {
				remaining = 100 - *l.CompletionPercent
			}
			if remaining < 0 {
				remaining = 0
			}
			out.SelfTestEntries = append(out.SelfTestEntries, model.SelfTestEntry{
				Type:      nvmeOperationName(*op.Value, op.String),
				Status:    op.String,
				Remaining: &remaining,
			})
		}

		for _, e := range l.Table {
			out.SelfTestEntries = append(out.SelfTestEntries, model.SelfTestEntry{
				Type:   enumString(e.SelfTestCode),
				Status: enumString(e.SelfTestResult),
				Passed: nvmePassed(e.SelfTestResult),
				Hours:  e.PowerOnHours,
			})
		}
		// The table is a bounded circular buffer, so its length is the count — and a table that is
		// present but empty is a drive that has never run a test, which is worth saying.
		if l.Table != nil {
			count := int64(len(l.Table))
			out.SelfTestCount = &count
		}
	}

	// The error log. `-x` reads the ATA *extended* log, which is where modern drives put
	// their errors; the summary log is the shorter one some drives answer instead, and it
	// may be reported as supported while being permanently empty.
	if c := ataLogCount(r.ATAErrorLog); c != nil {
		out.ErrorCount = c
	} else if r.NVMeHealth != nil && r.NVMeHealth.NumErrLogEntries != nil {
		// NVMe has no ATA-style log. The SMART/Health log carries the count of entries the
		// drive has logged; the Error Information log itself frequently cannot be read even
		// when that count is non-zero — a real SN730 reports 1 and emits no entries at all,
		// with or without `-l error`.
		out.ErrorCount = r.NVMeHealth.NumErrLogEntries
	}

	if c := ataLogCount(r.ATASelfTestLog); c != nil {
		out.SelfTestCount = c
	}

	// The entries themselves. The two logs share smartctl's nesting but not their contents: a
	// self-test entry carries the test, its outcome and when it ran, while an error entry on a
	// drive smartctl does not recognise carries a number and an index and nothing else.
	for _, e := range ataLogTable(r.ATAErrorLog) {
		out.ErrorEntries = append(out.ErrorEntries, model.ErrorLogEntry{
			Number: e.ErrorNumber,
			Index:  e.LogIndex,
			Hours:  e.LifetimeHours,
			LBA:    e.LBA,
			Status: enumString(e.Status),
		})
	}
	for _, e := range ataLogTable(r.ATASelfTestLog) {
		// `status` carries two fields the self-test log needs and nothing else does: whether
		// the test passed, and how much is left when one is still running.
		var passed *bool
		var remaining *int
		if e.Status != nil {
			passed, remaining = e.Status.Passed, e.Status.RemainingPercent
		}
		out.SelfTestEntries = append(out.SelfTestEntries, model.SelfTestEntry{
			Type:      enumString(e.Type),
			Status:    enumString(e.Status),
			Passed:    passed,
			Remaining: remaining,
			Hours:     e.LifetimeHours,
		})
	}

	// A test in progress is reported in one of two places, and drives differ about which. The
	// self-test log's newest row carries `remaining_percent` while one runs — but so does the drive's
	// own self-test-execution status in the SMART data structure, which is where the ATA spec puts it.
	// Reading only the log meant a drive that reports it in the status field looked idle mid-test:
	// no progress, and a card that offered the run buttons again for a drive already testing itself.
	if st := ataRunning(r.ATASmartData); st != nil && !anyRunning(out.SelfTestEntries) {
		out.SelfTestEntries = append([]model.SelfTestEntry{*st}, out.SelfTestEntries...)
	}

	if r.ATASmartData != nil {
		if caps := r.ATASmartData.Capabilities; caps != nil {
			supported := caps.SelfTestsSupported
			out.SelfTestSupported = &supported
		}
		if st := r.ATASmartData.SelfTest; st != nil {
			if st.PollingMinutes.Short > 0 {
				v := st.PollingMinutes.Short
				out.ShortMinutes = &v
			}
			if st.PollingMinutes.Extended > 0 {
				v := st.PollingMinutes.Extended
				out.ExtendedMinutes = &v
			}
		}
	}

	// smartctl's own words about a log it could not read. For an NVMe drive this is the
	// *only* signal that there is no self-test log: the request comes back
	// "Read Self-test Log failed: Invalid Field in Command" and nothing else reports it.
	for _, m := range r.Smartctl.Messages {
		if !strings.Contains(strings.ToLower(m.String), "self-test log failed") {
			continue
		}
		// The *log* could not be read, which is not the same finding as a drive that cannot test
		// itself — and this project has the receipts: the same SN730 under smartctl 7.4 reported
		// exactly this, and under 7.5 reads the log fine. Concluding "this drive cannot run
		// self-tests" from a failed read was a confident statement about the wrong thing, and it
		// told the reader to give up on a drive that tests itself perfectly well.
		//
		// Set on both fields because two panels explain two different absences with the one
		// sentence: the self-test card, and the logs card whose count is now missing.
		msg := strings.TrimSpace(m.String)
		out.SelfTestLogUnread = msg
		out.Note = msg
		break
	}

	return out
}

// unreadSelfTestLog names a self-test log present in the document that this parser does not read.
//
// Detected rather than decoded, and that is the whole design. The shape of smartctl's NVMe self-test
// log has never been observed on this project's hardware (`testdata/nvme-*.json` carry no self-test
// data at all), and guessing it would produce a panel that renders nothing precisely when a drive
// has something to say — the one moment it matters. Naming the gap is the useful half instead: a
// drive whose test ran and left no trace in the UI is indistinguishable from a test that never ran,
// and only one of those is true.
//
// The keys this parser *does* consume are excluded by name, so a drive whose log is read normally
// can never be reported as unread.
func unreadSelfTestLog(raw []byte) string {
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var found []string
	for key := range doc {
		// The two logs this parser *does* read, by name, so a drive whose log is read normally can
		// never be reported as unread.
		if !strings.Contains(key, "self_test") || key == "ata_smart_self_test_log" ||
			key == "nvme_self_test_log" {
			continue
		}
		found = append(found, key)
	}
	if len(found) == 0 {
		return ""
	}
	// Sorted, because a map's order is random and this string is shown to a person.
	sort.Strings(found)
	return strings.Join(found, ", ")
}

// rawATASmartData is the SMART Data structure's own summary of the drive.
type rawATASmartData struct {
	SelfTest *struct {
		// Status is the drive's *self-test execution status*, which the ATA spec defines as reporting
		// the currently executing test and how much of it is left. It also holds the last test's
		// outcome when nothing is running, which is what `passed` and the string are for.
		Status         *rawEnum `json:"status"`
		PollingMinutes struct {
			Short    int `json:"short"`
			Extended int `json:"extended"`
		} `json:"polling_minutes"`
	} `json:"self_test"`
	Capabilities *struct {
		SelfTestsSupported bool `json:"self_tests_supported"`
	} `json:"capabilities"`
}

// rawNVMeSelfTestLog is the NVMe self-test log. Verified against a real WDC SN730
// (`nvme-sn730-selftest.json`), which is the fixture this parser was written against.
//
// Its shape differs from the ATA log in the one way that mattered: the test *in progress* is a
// header field rather than a table row, and its progress is a separate number rather than a field on
// an entry. Reading the ATA log, finding nothing, and reporting "no test running" is how an NVMe
// drive mid-test came to look idle — which made the card offer the run buttons again, and the drive
// refuse the second one with a bare exit status.
type rawNVMeSelfTestLog struct {
	CurrentSelfTestOperation *struct {
		Value  *int   `json:"value"`
		String string `json:"string"`
	} `json:"current_self_test_operation"`
	CompletionPercent *int                   `json:"current_self_test_completion_percent"`
	Table             []rawNVMeSelfTestEntry `json:"table"`
}

// rawNVMeSelfTestEntry is one finished test. `self_test_code` says which test, `self_test_result`
// says how it ended, and neither is shaped like the ATA log's fields.
type rawNVMeSelfTestEntry struct {
	SelfTestCode   *rawEnum `json:"self_test_code"`
	SelfTestResult *rawEnum `json:"self_test_result"`
	PowerOnHours   *int64   `json:"power_on_hours"`
}

// ataRunning reads the drive's self-test-execution status as a running test, or nil.
//
// The signal is `remaining_percent`, the same one the self-test log rows use: the ATA spec puts the
// percentage in the low nibble of that status byte only while a test executes, and smartctl leaves
// the field out the rest of the time. The status object carries no test *type*, so the entry is left
// without one and the UI falls back to calling it a self-test.
func ataRunning(data *rawATASmartData) *model.SelfTestEntry {
	if data == nil || data.SelfTest == nil || data.SelfTest.Status == nil {
		return nil
	}
	s := data.SelfTest.Status
	if s.RemainingPercent == nil {
		// No percentage means nothing is executing; the field still holds the last test's outcome,
		// which is a finished result rather than a running one.
		return nil
	}
	return &model.SelfTestEntry{
		Status:    enumString(s),
		Remaining: s.RemainingPercent,
	}
}

// anyRunning reports whether an entry is a test in progress.
func anyRunning(entries []model.SelfTestEntry) bool {
	for _, e := range entries {
		if e.Remaining != nil {
			return true
		}
	}
	return false
}

// nvmeOperationName names a self-test in progress, from the NVMe base spec's Device Self-test
// Operation values. The drive's own string is the fallback, because an unrecognised code is the
// drive telling us about a test this app has not heard of, and its words beat a blank.
func nvmeOperationName(value int, fallback string) string {
	switch value {
	case 1:
		return "Short"
	case 2:
		return "Extended"
	case 3:
		return "Vendor specific"
	}
	return fallback
}

// nvmePassed reads the NVMe base spec's Device Self-test Result code.
//
// Only the two unambiguous ends are claimed. Codes 1–6 are **aborts** — a controller reset, a
// format, a host abort — and an abort is not a failure of the drive, so they are left without a
// verdict rather than rendered as one; the drive's own words are shown either way, which is where
// "Aborted due to a Controller Level Reset" reads as exactly what it is.
func nvmePassed(result *rawEnum) *bool {
	if result == nil || result.Value == nil {
		return nil
	}
	switch *result.Value {
	case 0: // Completed without error.
		yes := true
		return &yes
	case 7, 15: // Could not complete the operation; unknown.
		no := false
		return &no
	}
	return nil
}

// ataLogCount returns the entry count from whichever variant of an ATA log the drive answered with,
// preferring the extended log.
func ataLogCount(logs *rawATALogs) *int64 {
	if logs == nil {
		return nil
	}
	for _, variant := range []*rawATALog{logs.Extended, logs.Standard, logs.Summary} {
		if variant != nil && variant.Count != nil {
			return variant.Count
		}
	}
	return nil
}

type rawLogDoc struct {
	ATASmartData   *rawATASmartData `json:"ata_smart_data"`
	ATAErrorLog    *rawATALogs      `json:"ata_smart_error_log"`
	ATASelfTestLog *rawATALogs      `json:"ata_smart_self_test_log"`
	NVMeHealth     *struct {
		NumErrLogEntries *int64 `json:"num_err_log_entries"`
	} `json:"nvme_smart_health_information_log"`
	// SelfTest is the drive's own answer to "do you implement self-tests?" — a direct statement
	// rather than an inference from whether a log could be read.
	NVMeAdminCommands *struct {
		SelfTest *bool `json:"self_test"`
	} `json:"nvme_optional_admin_commands"`
	NVMeSelfTestLog *rawNVMeSelfTestLog `json:"nvme_self_test_log"`
	Smartctl        struct {
		Messages []struct {
			Severity string `json:"severity"`
			String   string `json:"string"`
		} `json:"messages"`
	} `json:"smartctl"`
}

// rawATALogs is the shape both ATA logs share. smartctl nests each log under a log-type key, and
// which key appears depends on the log *and* the drive:
//
//   - the error log answers `summary` (the shorter log) or `extended`;
//   - the self-test log answers `standard` (the shorter one) or `extended`.
//
// Reading only `extended` and `summary` meant a drive whose self-test log is the `standard` variant
// reported no self-test log at all — no count, no entries, and no sign of a test in progress, so the
// card offered the run buttons again for a drive that was already testing itself.
type rawATALogs struct {
	Standard *rawATALog `json:"standard"`
	Summary  *rawATALog `json:"summary"`
	Extended *rawATALog `json:"extended"`
}

type rawATALog struct {
	Count *int64 `json:"count"`
	// Table holds the entries. smartctl omits it entirely when the log is empty, which is the
	// case the counts-only version of this panel was built around.
	Table []rawATAEntry `json:"table"`
}

// rawATAEntry is one row of either ATA log. Both logs share this shape; the fields are simply
// absent where the drive does not supply them.
type rawATAEntry struct {
	ErrorNumber   *int64   `json:"error_number"`
	LogIndex      *int64   `json:"log_index"`
	LifetimeHours *int64   `json:"lifetime_hours"`
	LBA           *uint64  `json:"lba"`
	Type          *rawEnum `json:"type"`
	Status        *rawEnum `json:"status"`
}

// rawEnum is smartctl's shape for anything with a machine value and a human string. A status
// carries two extra fields that only the self-test log needs, and NVMe entries carry the value.
type rawEnum struct {
	Value            *int   `json:"value"`
	String           string `json:"string"`
	Passed           *bool  `json:"passed"`
	RemainingPercent *int   `json:"remaining_percent"`
}

// ataLogTable returns the entries from whichever variant of an ATA log the drive answered with.
func ataLogTable(logs *rawATALogs) []rawATAEntry {
	if logs == nil {
		return nil
	}
	for _, variant := range []*rawATALog{logs.Extended, logs.Standard, logs.Summary} {
		if variant != nil && len(variant.Table) > 0 {
			return variant.Table
		}
	}
	return nil
}

func enumString(e *rawEnum) string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.String)
}
