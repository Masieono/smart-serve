package model

// LogSummary is what a drive's own logs say about past trouble.
//
// The counts are the headline, and they are the part verified against real hardware: a
// non-zero error count is the difference between "this drive has never complained" and
// "this drive has complained". The entry lists themselves are deliberately not modelled
// here — see `internal/smart/logs.go` for why.
type LogSummary struct {
	// ErrorCount is how many entries the drive's error log holds. Absent when the drive
	// reports no error log at all, which is a different finding from an empty one.
	ErrorCount *int64 `json:"error_count,omitempty"`
	// SelfTestCount is how many self-test results the drive is holding on to.
	SelfTestCount *int64 `json:"self_test_count,omitempty"`
	// SelfTestSupported is false when the drive has told us it cannot do self-tests, or
	// that its self-test log cannot be read. Absent when nothing said either way.
	SelfTestSupported *bool `json:"self_test_supported,omitempty"`
	// ShortMinutes and ExtendedMinutes are the drive's *own* estimate of how long its
	// tests run. This is the progress basis a running test needs: a long test on a large
	// drive runs for hours, and a spinner with no percentage is indistinguishable from a
	// hang (`FEATURES.md` A19).
	ShortMinutes    *int `json:"short_minutes,omitempty"`
	ExtendedMinutes *int `json:"extended_minutes,omitempty"`
	// Note carries smartctl's own words when a log could not be read, because that is
	// usually the whole explanation.
	Note string `json:"note,omitempty"`
	// SelfTestLogUnread names a self-test log the drive has that this app does not read, if any.
	// Present means the drive may be able to test itself but its progress and past results cannot
	// be shown here — a different statement from "this drive has no self-test log", and the only
	// one of the two that the reader can act on.
	SelfTestLogUnread string `json:"self_test_log_unread,omitempty"`

	// ErrorEntries and SelfTestEntries are what the logs actually hold, in the order the
	// drive reports them (newest first).
	ErrorEntries    []ErrorLogEntry `json:"error_entries,omitempty"`
	SelfTestEntries []SelfTestEntry `json:"self_test_entries,omitempty"`
}

// SelfTestEntry is one row of a drive's self-test log.
//
// Unlike the error log, this one is worth rendering: the entries carry the test type, its
// outcome, how far it got, and when it ran. It is also the record `A19` will add to.
type SelfTestEntry struct {
	// Type is the drive's own words for the test: "Short offline", "Extended offline", and so
	// on. Not enumerated here, because the vocabulary is the drive's and inventing a mapping
	// would lose whatever it actually said.
	Type string `json:"type,omitempty"`
	// Status is the drive's own words for the outcome, including "Self-test routine in
	// progress" while one is running.
	Status string `json:"status,omitempty"`
	// Passed is present only when the drive states it. It is absent for a test still running,
	// which is not the same finding as a failure.
	Passed *bool `json:"passed,omitempty"`
	// Remaining is the percentage left, present only while a test is in progress. This is the
	// progress reading `A19` has to show: a spinner with no figure is indistinguishable from a
	// hang, and a long test on a large drive runs for hours.
	Remaining *int `json:"remaining,omitempty"`
	// Hours is the drive's lifetime hours when the test ran, which is also the order the
	// entries arrive in.
	Hours *int64 `json:"hours,omitempty"`
}

// ErrorLogEntry is one row of a drive's error log.
//
// Deliberately sparse, and that sparseness is the finding rather than an omission. On a drive
// smartctl does not recognise, a logged error carries an error number and an index and nothing
// else — no LBA, no timestamp, no status — so a row per entry can be a row of dashes. The count
// is the useful part; these exist so a bare number does not go unexplained.
type ErrorLogEntry struct {
	Number *int64  `json:"number,omitempty"`
	Index  *int64  `json:"index,omitempty"`
	Hours  *int64  `json:"hours,omitempty"`
	LBA    *uint64 `json:"lba,omitempty"`
	Status string  `json:"status,omitempty"`
}

// HasDetail reports whether an error-log entry says anything beyond which entry it is.
//
// The panel uses it to decide between a table and a sentence: a table of nothing but numbers
// adds no information the count has not already given.
func (e ErrorLogEntry) HasDetail() bool {
	return e.Hours != nil || e.LBA != nil || e.Status != ""
}

// IsEmpty reports whether a summary holds nothing worth showing.
// A method rather than a comparison against the zero value, because the summary contains slices
// and is no longer comparable. It exists so a caller can tell "the drive reported nothing" from
// "the drive reported that it has nothing" — the two look identical in an empty panel, and only
// the first is a reason not to draw one.
func (s LogSummary) IsEmpty() bool {
	return s.ErrorCount == nil &&
		s.SelfTestCount == nil &&
		s.SelfTestSupported == nil &&
		s.ShortMinutes == nil &&
		s.ExtendedMinutes == nil &&
		s.Note == "" &&
		s.SelfTestLogUnread == "" &&
		len(s.ErrorEntries) == 0 &&
		len(s.SelfTestEntries) == 0
}

// LatestFinished returns the newest self-test result that has finished, or nil.
//
// "Newest" is the drive's own ordering, because a self-test log is reported newest first, and
// "finished" means the entry carries no percentage remaining — that field appears only while a
// test runs. This is the entry `B9` compares against next time: a result that is still going is
// not yet news, and one the drive reports no verdict on is not a failure.
func (s LogSummary) LatestFinished() *SelfTestEntry {
	for i := range s.SelfTestEntries {
		if s.SelfTestEntries[i].Remaining == nil {
			return &s.SelfTestEntries[i]
		}
	}
	return nil
}

// Running returns the self-test currently in progress, or nil.
//
// A test in progress is identified by the drive reporting a **percentage remaining**, because
// that field appears only while one runs. Its `passed` field is deliberately not the signal: a
// real drive reports `passed: true` for a test that is still going, so treating that as
// "finished" would hide a running test behind a green tick.
func (s LogSummary) Running() *SelfTestEntry {
	for i := range s.SelfTestEntries {
		if s.SelfTestEntries[i].Remaining != nil {
			return &s.SelfTestEntries[i]
		}
	}
	return nil
}
