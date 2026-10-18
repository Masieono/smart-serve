package smart

import (
	"encoding/json"
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
		unsupported := false
		out.SelfTestSupported = &unsupported
		out.Note = strings.TrimSpace(m.String)
		break
	}

	return out
}

// ataLogCount returns the entry count from whichever variant of an ATA log the drive
// answered with, preferring the extended log.
func ataLogCount(logs *rawATALogs) *int64 {
	if logs == nil {
		return nil
	}
	if logs.Extended != nil && logs.Extended.Count != nil {
		return logs.Extended.Count
	}
	if logs.Summary != nil && logs.Summary.Count != nil {
		return logs.Summary.Count
	}
	return nil
}

type rawLogDoc struct {
	ATASmartData *struct {
		SelfTest *struct {
			PollingMinutes struct {
				Short    int `json:"short"`
				Extended int `json:"extended"`
			} `json:"polling_minutes"`
		} `json:"self_test"`
		Capabilities *struct {
			SelfTestsSupported bool `json:"self_tests_supported"`
		} `json:"capabilities"`
	} `json:"ata_smart_data"`
	ATAErrorLog    *rawATALogs `json:"ata_smart_error_log"`
	ATASelfTestLog *rawATALogs `json:"ata_smart_self_test_log"`
	NVMeHealth     *struct {
		NumErrLogEntries *int64 `json:"num_err_log_entries"`
	} `json:"nvme_smart_health_information_log"`
	Smartctl struct {
		Messages []struct {
			Severity string `json:"severity"`
			String   string `json:"string"`
		} `json:"messages"`
	} `json:"smartctl"`
}

// rawATALogs is the shape both ATA logs share. smartctl nests each log under a log-type key
// — `extended` for the comprehensive log `-x` reads, `summary` for the shorter one — so both
// are checked rather than assuming the flag decided it.
type rawATALogs struct {
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
// carries two extra fields that only the self-test log needs.
type rawEnum struct {
	String           string `json:"string"`
	Passed           *bool  `json:"passed"`
	RemainingPercent *int   `json:"remaining_percent"`
}

// ataLogTable returns the entries from whichever variant of an ATA log the drive answered with.
func ataLogTable(logs *rawATALogs) []rawATAEntry {
	if logs == nil {
		return nil
	}
	if logs.Extended != nil && len(logs.Extended.Table) > 0 {
		return logs.Extended.Table
	}
	if logs.Summary != nil && len(logs.Summary.Table) > 0 {
		return logs.Summary.Table
	}
	return nil
}

func enumString(e *rawEnum) string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.String)
}
