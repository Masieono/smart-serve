package sched

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/store"
)

// fakeDigester records digest sends instead of dispatching them.
type fakeDigester struct {
	mu      sync.Mutex
	sends   []store.Schedule
	started chan struct{}
}

func newFakeDigester() *fakeDigester {
	return &fakeDigester{started: make(chan struct{}, 16)}
}

func (f *fakeDigester) SendDigest(_ context.Context, sched store.Schedule) error {
	f.mu.Lock()
	f.sends = append(f.sends, sched)
	f.mu.Unlock()
	select {
	case f.started <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeDigester) sent() []store.Schedule {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Schedule(nil), f.sends...)
}

// createDigestSchedule makes a due digest schedule, which is the shape both tests below need.
func createDigestSchedule(t *testing.T, st *store.Store) int64 {
	t.Helper()
	seconds := int64(3600)
	id, err := st.CreateSchedule(context.Background(), store.Schedule{
		Enabled: true, Kind: "interval", EverySeconds: &seconds,
		Action: store.ActionDigest, Label: "weekly digest",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if err := st.SetScheduleNextRun(context.Background(), id,
		time.Now().Add(150*time.Millisecond)); err != nil {
		t.Fatalf("SetScheduleNextRun: %v", err)
	}
	return id
}

// waitForLastRun polls until the schedule has been advanced, which happens after the work returns.
func waitForLastRun(t *testing.T, st *store.Store, id int64) *store.Schedule {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		sched, err := st.GetSchedule(context.Background(), id)
		if err != nil {
			t.Fatalf("GetSchedule: %v", err)
		}
		if sched != nil && sched.LastRun != nil {
			return sched
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run was never recorded, so the schedule would fire again immediately")
	return nil
}

// B13: the action decides what a fire *means*. A digest schedule must send a digest and **not** scan —
// the two produce completely different things from the same timing, and running a scan as well would
// make a weekly summary quietly re-read every drive.
func TestADigestScheduleDigestsInsteadOfScanning(t *testing.T) {
	st := newTestStore(t)
	id := createDigestSchedule(t, st)

	runner := newFakeRunner()
	digester := newFakeDigester()
	s := New(st, runner, events.NewHub(), Options{Logger: discardLogger(), Digester: digester})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	select {
	case <-digester.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the digest schedule never fired")
	}

	if n := runner.count(); n != 0 {
		t.Errorf("a digest schedule ran %d scans, want none", n)
	}
	sent := digester.sent()
	if len(sent) != 1 || sent[0].ID != id || sent[0].Action != store.ActionDigest {
		t.Errorf("digests sent = %+v, want one for the digest schedule", sent)
	}
	waitForLastRun(t, st, id)
}

// A digest schedule with nowhere to send must still advance. The boundary arrived, and a schedule that
// stopped advancing would fire in a tight loop — so a missing sender is a log line, not a failure.
func TestADigestScheduleWithoutASenderStillAdvances(t *testing.T) {
	st := newTestStore(t)
	id := createDigestSchedule(t, st)

	runner := newFakeRunner()
	s := New(st, runner, events.NewHub(), Options{Logger: discardLogger()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	sched := waitForLastRun(t, st, id)
	if sched.NextRun == nil || !sched.NextRun.After(*sched.LastRun) {
		t.Errorf("next run = %v, want it moved past last run %v", sched.NextRun, sched.LastRun)
	}
	if n := runner.count(); n != 0 {
		t.Errorf("a digest schedule ran %d scans, want none", n)
	}
}
