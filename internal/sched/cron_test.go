package sched

import (
	"testing"
	"time"
)

func TestNextDailyAtThree(t *testing.T) {
	loc := time.UTC
	after := time.Date(2026, 10, 12, 1, 30, 0, 0, loc)

	got, err := Next("0 3 * * *", after, loc)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 10, 12, 3, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

// The boundary is exclusive: a scan that just ran at 03:00 must not be
// rescheduled for 03:00 the same minute, or it would fire in a tight loop.
func TestNextIsStrictlyAfter(t *testing.T) {
	after := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)

	got, err := Next("0 3 * * *", after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestNextSupportsStepsListsAndRanges(t *testing.T) {
	after := time.Date(2026, 10, 12, 8, 45, 0, 0, time.UTC)

	got, err := Next("30 8-9,18 * * *", after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestNextStepFromZero(t *testing.T) {
	after := time.Date(2026, 10, 12, 10, 7, 0, 0, time.UTC)

	got, err := Next("*/15 * * * *", after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 10, 12, 10, 15, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

// 2026-10-12 is a Monday, so "0 9 * * 1" from Monday 10:00 lands the following
// Monday.
func TestNextDayOfWeek(t *testing.T) {
	after := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

	got, err := Next("0 9 * * 1", after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

// Both day fields restricted means "either matches" in standard cron. From
// Tuesday 2026-10-06 the next Monday (the 12th) comes before the next 1st
// (November 1st), so the 12th wins.
func TestNextDayOfMonthOrDayOfWeek(t *testing.T) {
	after := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)

	got, err := Next("0 0 1 * 1", after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

// A sparse expression must resolve rather than spin: 29 February only exists in
// leap years.
func TestNextLeapDay(t *testing.T) {
	after := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)

	got, err := Next("0 3 29 2 *", after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2028, 2, 29, 3, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestNextAcceptsSevenForSunday(t *testing.T) {
	after := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) // Saturday

	got, err := Next("0 0 * * 7", after, time.UTC)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) // Sunday
	if !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
}

func TestNextRejectsMalformedExpressions(t *testing.T) {
	for _, expr := range []string{
		"",             // nothing
		"* * * *",      // four fields
		"* * * * * *",  // six fields
		"60 * * * *",   // minute out of range
		"* 24 * * *",   // hour out of range
		"0 0 0 * *",    // day of month out of range
		"0 0 * 13 *",   // month out of range
		"*/0 * * * *",  // zero step
		"a * * * *",    // not a number
		"5-1 * * * *",  // inverted range
		"1-99 * * * *", // range beyond the field
	} {
		if _, err := Next(expr, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), time.UTC); err == nil {
			t.Errorf("Next(%q) should have failed", expr)
		}
	}
}
