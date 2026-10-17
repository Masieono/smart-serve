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
}
