package sched

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // embed the tz database so timezone tests never depend on the host

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/store"
)

// fakeRunner records scans instead of touching hardware.
type fakeRunner struct {
	mu       sync.Mutex
	triggers []scan.Trigger
	started  chan struct{}
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{started: make(chan struct{}, 16)}
}

func (f *fakeRunner) Scan(_ context.Context, t scan.Trigger) (scan.Result, bool, error) {
	f.mu.Lock()
	f.triggers = append(f.triggers, t)
	n := int64(len(f.triggers))
	f.mu.Unlock()

	select {
	case f.started <- struct{}{}:
	default:
	}
	return scan.Result{ScanID: n, OK: true}, false, nil
}

func (f *fakeRunner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.triggers)
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

func mustCreateInterval(t *testing.T, st *store.Store, seconds int64, enabled bool) int64 {
	t.Helper()
	id, err := st.CreateSchedule(context.Background(), store.Schedule{
		Enabled: enabled, Kind: "interval", EverySeconds: &seconds, Label: "test",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	return id
}

// An interval schedule with no stored boundary gets one, and it is persisted so
// a restart does not recompute a different answer.
func TestNextDueArmsANewSchedule(t *testing.T) {
	st := newTestStore(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	id := mustCreateInterval(t, st, 600, true)

	s := New(st, newFakeRunner(), events.NewHub(), Options{Now: fixedClock(now)})
	next, gotID, ok := s.nextDue(context.Background())
	if !ok || gotID != id {
		t.Fatalf("nextDue = (%v, %d, %v), want a due schedule %d", next, gotID, ok, id)
	}
	want := now.Add(10 * time.Minute)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v", next, want)
	}

	sched, err := st.GetSchedule(context.Background(), id)
	if err != nil || sched == nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if sched.NextRun == nil || !sched.NextRun.Equal(want) {
		t.Errorf("persisted next_run = %v, want %v", sched.NextRun, want)
	}
}

func TestNextDueIgnoresDisabledSchedules(t *testing.T) {
	st := newTestStore(t)
	mustCreateInterval(t, st, 60, false)

	s := New(st, newFakeRunner(), events.NewHub(), Options{Now: fixedClock(time.Now())})
	if _, _, ok := s.nextDue(context.Background()); ok {
		t.Error("a disabled schedule should not be due")
	}
}

// A schedule whose next run is still ahead keeps it, rather than being pushed
// further out on every evaluation.
func TestNextDueKeepsAFutureBoundary(t *testing.T) {
	st := newTestStore(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	id := mustCreateInterval(t, st, 3600, true)
	boundary := now.Add(20 * time.Minute)
	if err := st.SetScheduleNextRun(context.Background(), id, boundary); err != nil {
		t.Fatalf("SetScheduleNextRun: %v", err)
	}

	s := New(st, newFakeRunner(), events.NewHub(), Options{Now: fixedClock(now)})
	next, _, ok := s.nextDue(context.Background())
	if !ok || !next.Equal(boundary) {
		t.Errorf("next = %v, want the existing boundary %v", next, boundary)
	}
}

func TestCronUsesTheConfiguredTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateSchedule(ctx, store.Schedule{
		Enabled: true, Kind: "cron", Cron: "0 3 * * *", Label: "nightly",
	}); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	now := time.Date(2026, 10, 12, 1, 0, 0, 0, loc)
	s := New(st, newFakeRunner(), events.NewHub(), Options{Location: loc, Now: fixedClock(now)})
	next, _, ok := s.nextDue(ctx)
	if !ok {
		t.Fatal("expected a due schedule")
	}
	want := time.Date(2026, 10, 12, 3, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v (03:00 local)", next, want)
	}
	if next.Hour() != 3 {
		t.Errorf("next hour = %d, want 3 — cron must be evaluated in local time", next.Hour())
	}
}

// The default policy must not fire a burst of catch-up scans after downtime: an
// overdue boundary is reconciled once, at start-up, and pushed into the future.
func TestStartupSkipsAMissedRun(t *testing.T) {
	st := newTestStore(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	id := mustCreateInterval(t, st, 3600, true)
	if err := st.SetScheduleNextRun(context.Background(), id, now.Add(-3*time.Hour)); err != nil {
		t.Fatalf("SetScheduleNextRun: %v", err)
	}

	s := New(st, newFakeRunner(), events.NewHub(), Options{
		Now: fixedClock(now), MissedRunPolicy: "skip", Logger: discardLogger(),
	})
	s.reconcileStartup(context.Background())

	next, _, ok := s.nextDue(context.Background())
	if !ok {
		t.Fatal("expected a due schedule")
	}
	if !next.Equal(now.Add(time.Hour)) {
		t.Errorf("next = %v, want %v (one interval from now)", next, now.Add(time.Hour))
	}
	if next.Before(now) {
		t.Error("a skipped run must be rescheduled into the future")
	}

	sched, err := st.GetSchedule(context.Background(), id)
	if err != nil || sched == nil || sched.NextRun == nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if !sched.NextRun.Equal(next) {
		t.Errorf("persisted next_run = %v, want %v", sched.NextRun, next)
	}
}

// "run_once" deliberately leaves the overdue boundary alone, so the loop fires it
// once and then advances normally.
func TestStartupRunOnceKeepsTheOverdueBoundary(t *testing.T) {
	st := newTestStore(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	overdue := now.Add(-3 * time.Hour)
	id := mustCreateInterval(t, st, 3600, true)
	if err := st.SetScheduleNextRun(context.Background(), id, overdue); err != nil {
		t.Fatalf("SetScheduleNextRun: %v", err)
	}

	s := New(st, newFakeRunner(), events.NewHub(), Options{
		Now: fixedClock(now), MissedRunPolicy: "run_once", Logger: discardLogger(),
	})
	s.reconcileStartup(context.Background())

	next, _, ok := s.nextDue(context.Background())
	if !ok || !next.Equal(overdue) {
		t.Errorf("next = %v, want the overdue time %v so it fires once", next, overdue)
	}
}

// A boundary that arrives while the loop is watching must fire, not be mistaken
// for a missed run. This is the same path "run now" takes.
func TestBoundaryDueNowFiresRatherThanReschedules(t *testing.T) {
	st := newTestStore(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	id := mustCreateInterval(t, st, 3600, true)
	if err := st.SetScheduleNextRun(context.Background(), id, now); err != nil {
		t.Fatalf("SetScheduleNextRun: %v", err)
	}

	s := New(st, newFakeRunner(), events.NewHub(), Options{
		Now: fixedClock(now), Logger: discardLogger(),
	})
	next, _, ok := s.nextDue(context.Background())
	if !ok {
		t.Fatal("expected a due schedule")
	}
	if !next.Equal(now) {
		t.Errorf("next = %v, want the past boundary %v returned as-is", next, now)
	}
}

// A boundary that arrives while the loop is watching must fire the scan and be
// recorded, so the UI can show a truthful last-run and next boundary.
func TestSchedulerFiresAndRecordsTheRun(t *testing.T) {
	st := newTestStore(t)
	id := mustCreateInterval(t, st, 3600, true)

	// A boundary a moment ahead: the loop must sleep to it and fire, rather than
	// treating it as a missed run.
	boundary := time.Now().Add(150 * time.Millisecond)
	if err := st.SetScheduleNextRun(context.Background(), id, boundary); err != nil {
		t.Fatalf("SetScheduleNextRun: %v", err)
	}

	runner := newFakeRunner()
	s := New(st, runner, events.NewHub(), Options{Logger: discardLogger()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the schedule never fired")
	}

	// Bookkeeping happens after the scan returns, so poll briefly.
	var sched *store.Schedule
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		got, err := st.GetSchedule(context.Background(), id)
		if err != nil {
			t.Fatalf("GetSchedule: %v", err)
		}
		if got != nil && got.LastRun != nil {
			sched = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sched == nil || sched.LastRun == nil {
		t.Fatal("last_run was never recorded")
	}
	if delta := time.Since(*sched.LastRun); delta < 0 || delta > time.Minute {
		t.Errorf("last_run = %v, want roughly now", sched.LastRun)
	}
	if sched.NextRun == nil || !sched.NextRun.Equal(sched.LastRun.Add(time.Hour)) {
		t.Errorf("next_run = %v, want last_run + 1h (%v)", sched.NextRun, sched.LastRun.Add(time.Hour))
	}

	runner.mu.Lock()
	trigger := runner.triggers[0]
	runner.mu.Unlock()
	if trigger.Kind != "schedule" || trigger.ScheduleID != id {
		t.Errorf("trigger = %+v, want a schedule trigger for %d", trigger, id)
	}
}

func TestRunNowFiresImmediately(t *testing.T) {
	st := newTestStore(t)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	id := mustCreateInterval(t, st, 3600, true)
	if err := st.SetScheduleNextRun(context.Background(), id, now.Add(time.Hour)); err != nil {
		t.Fatalf("SetScheduleNextRun: %v", err)
	}

	runner := newFakeRunner()
	s := New(st, runner, events.NewHub(), Options{Now: fixedClock(now), Logger: discardLogger()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	if err := s.RunNow(context.Background(), id); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("run now never fired")
	}
}

// Wake makes the loop re-evaluate immediately, so a schedule created while it was
// asleep is armed without waiting for the next boundary.
func TestWakeArmsANewSchedule(t *testing.T) {
	st := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := New(st, newFakeRunner(), events.NewHub(), Options{Now: time.Now, Logger: discardLogger()})
	go s.Run(ctx)

	// A schedule an hour out: the loop has nothing to do until then.
	every := int64(3600)
	id, err := st.CreateSchedule(ctx, store.Schedule{
		Enabled: true, Kind: "interval", EverySeconds: &every, Label: "hourly",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	s.Wake()

	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		sched, err := st.GetSchedule(context.Background(), id)
		if err != nil {
			t.Fatalf("GetSchedule: %v", err)
		}
		if sched != nil && sched.NextRun != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the wake-up did not arm the new schedule")
}

// With nothing scheduled the loop must sleep rather than spin, and it must still
// stop promptly when the process shuts down (the zero-idle-cost invariant).
func TestRunSleepsWithoutSchedulesAndStopsOnCancel(t *testing.T) {
	st := newTestStore(t)
	runner := newFakeRunner()
	s := New(st, runner, events.NewHub(), Options{Now: time.Now, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Run returned while schedules could still be added")
	case <-time.After(100 * time.Millisecond):
	}
	if runner.count() != 0 {
		t.Errorf("scans = %d, want 0", runner.count())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop on cancellation")
	}
}
