package store

import (
	"context"
	"testing"
	"time"
)

// scanAt writes one scan with a single drive at a given verdict, so a test can build a history.
func scanAt(t *testing.T, s *Store, at time.Time, serial, health string) {
	t.Helper()
	if _, err := s.SaveScan(context.Background(), "manual", at, at.Add(time.Second), []ScannedDevice{{
		Serial: serial, Model: "M", DevicePath: "/dev/sda", Transport: "sata",
		Health: health, Score: 50,
	}}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
}

// failScanAt records a failed scan. `SaveScan` only writes successful ones — a failure is recorded by
// the scanner itself, which has the error text — so the test writes the row it is testing against.
func failScanAt(t *testing.T, s *Store, at time.Time) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(), `
		INSERT INTO scan_history (started_at, ended_at, trigger, ok, error, devices_total, devices_ok, devices_failed)
		VALUES (?, ?, 'schedule', 0, 'lsblk failed', 0, 0, 0)`,
		at.UnixMilli(), at.Add(time.Second).UnixMilli()); err != nil {
		t.Fatalf("insert failed scan: %v", err)
	}
}

// B13's baseline: each drive's verdict as of a moment in the past, so a digest can say what *changed*
// without a second history — the snapshots are already the history, and the verdict is on every one.
func TestHealthAtReadsTheVerdictAsOfAMoment(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	scanAt(t, s, base, "a", "good")
	scanAt(t, s, base.Add(time.Hour), "a", "caution") // the newest one before the cut
	scanAt(t, s, base.Add(2*time.Hour), "a", "bad")   // after it
	scanAt(t, s, base.Add(time.Hour), "b", "unknown")
	// A drive first seen *after* the cut has no baseline, and must be absent rather than guessed at.
	scanAt(t, s, base.Add(3*time.Hour), "c", "good")

	got, err := s.HealthAt(context.Background(), base.Add(90*time.Minute))
	if err != nil {
		t.Fatalf("HealthAt: %v", err)
	}
	if got["a"] != "caution" {
		t.Errorf("a = %q, want the newest reading at or before the cut (caution)", got["a"])
	}
	if got["b"] != "unknown" {
		t.Errorf("b = %q, want unknown", got["b"])
	}
	if _, ok := got["c"]; ok {
		t.Errorf("c = %q, want it absent — it did not exist at the cut", got["c"])
	}
}

// Both figures matter for a digest: "7 completed" is the reassuring half, and "1 failed" is the one
// that explains a gap in the history.
func TestScanCountsBoundsThePeriod(t *testing.T) {
	s := newTestStore(t)
	from := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	scanAt(t, s, from.Add(-time.Hour), "a", "good") // before the period
	scanAt(t, s, from, "a", "good")
	scanAt(t, s, from.Add(24*time.Hour), "a", "good")
	failScanAt(t, s, from.Add(48*time.Hour))
	// Ends exactly at `to`, so it is inside the period — the bound is inclusive on both sides.
	scanAt(t, s, to.Add(-time.Second), "a", "good")
	scanAt(t, s, to.Add(time.Hour), "a", "good") // after the period

	ok, failed, err := s.ScanCounts(context.Background(), from, to)
	if err != nil {
		t.Fatalf("ScanCounts: %v", err)
	}
	if ok != 3 || failed != 1 {
		t.Errorf("ok = %d, failed = %d; want 3 and 1", ok, failed)
	}
}

// A store with nothing in it answers zero rather than an error: a first digest is a real message, and
// it should say the fleet is empty rather than fail to send.
func TestDigestQueriesOnAnEmptyStore(t *testing.T) {
	s := newTestStore(t)

	got, err := s.HealthAt(context.Background(), time.Now())
	if err != nil || len(got) != 0 {
		t.Errorf("HealthAt = %v, %v; want an empty map and no error", got, err)
	}
	ok, failed, err := s.ScanCounts(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err != nil || ok != 0 || failed != 0 {
		t.Errorf("ScanCounts = %d, %d, %v; want zeroes and no error", ok, failed, err)
	}
}
