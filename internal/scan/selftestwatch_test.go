package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// The self-test watcher (`FEATURES.md` A20).
//
// It is a new concurrency path that reads drives on a timer, so it is tested rather than reasoned
// about: the fleet badge is only as current as this loop, and the report was that it was not current
// at all.

/** A store with one drive recorded as testing itself. */
func watchFixture(t *testing.T, running bool, pct *int, kind string) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if _, err := st.SaveScan(ctx, "manual", at, at.Add(time.Second), []store.ScannedDevice{{
		Serial: "S", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
		SelfTestRunning: running, SelfTestPct: pct, SelfTestKind: kind,
	}}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
	return st
}

/** A Manager whose smartctl call returns a fixture instead of running one. */
func watchManager(t *testing.T, st *store.Store, hub *events.Hub, reply func() (smart.Result, error)) *Manager {
	t.Helper()
	m := NewManager(nil, st, hub)
	m.SetSelfTestReader(func(context.Context, string, string, time.Duration) (smart.Result, error) {
		return reply()
	})
	return m
}

/** The fixture bytes, read from the smart package's own testdata — the same files its parser is
 *  tested against, so the watcher is not asserting against a second, invented copy of the format. */
func fixture(t *testing.T, name string) smart.Result {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "smart", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return smart.Result{Stdout: raw}
}

// The drive's progress reaches the store, which is what the fleet badge reads.
func TestTheWatcherRecordsMovingProgress(t *testing.T) {
	// The stored figure is stale — the last scan caught 3% — and the drive now says 10%.
	stale := 3
	st := watchFixture(t, true, &stale, "")
	hub := events.NewHub()

	m := watchManager(t, st, hub, func() (smart.Result, error) {
		return fixture(t, "ata-selftest-running.json"), nil
	})
	m.watchSelfTestsOnce(context.Background())

	state, err := st.SelfTestState(context.Background(), "S")
	if err != nil {
		t.Fatalf("SelfTestState: %v", err)
	}
	if !state.Running {
		t.Fatal("the watcher lost a running test")
	}
	if state.Pct == nil || *state.Pct != 10 {
		t.Errorf("pct = %v, want 10 (the fixture reports 90%% remaining)", state.Pct)
	}
}

// A test that has ended stops being a running test, and the badge goes with it.
func TestTheWatcherClearsAFinishedTest(t *testing.T) {
	started := 10
	st := watchFixture(t, true, &started, "")

	m := watchManager(t, st, events.NewHub(), func() (smart.Result, error) {
		// This fixture's own self-test status says nothing is executing.
		return fixture(t, "ata-bto-1tb-logs.json"), nil
	})
	m.watchSelfTestsOnce(context.Background())

	state, err := st.SelfTestState(context.Background(), "S")
	if err != nil {
		t.Fatalf("SelfTestState: %v", err)
	}
	if state.Running {
		t.Error("a finished test is still recorded as running; the badge would never clear")
	}
	if state.Pct != nil {
		t.Errorf("pct = %v, want nil once nothing is running", *state.Pct)
	}
}

// Only *news* is published. A page re-rendered every interval for an unchanged reading would take the
// fleet's search box out from under whoever was typing in it.
func TestTheWatcherOnlyPublishesChanges(t *testing.T) {
	// Exactly what the fixture reports, so the first pass has nothing to say.
	pct := 10
	st := watchFixture(t, true, &pct, "")
	hub := events.NewHub()
	subID, ch := hub.Subscribe()
	defer hub.Unsubscribe(subID)

	m := watchManager(t, st, hub, func() (smart.Result, error) {
		return fixture(t, "ata-selftest-running.json"), nil
	})
	m.watchSelfTestsOnce(context.Background())
	select {
	case e := <-ch:
		t.Fatalf("published %q for an unchanged reading", e.Type)
	default:
	}

	// Move the drive. Now it is news.
	m.SetSelfTestReader(func(context.Context, string, string, time.Duration) (smart.Result, error) {
		raw := `{"ata_smart_data":{"self_test":{"status":{"value":249,` +
			`"string":"Self-test routine in progress","remaining_percent":40}}}}`
		return smart.Result{Stdout: []byte(raw)}, nil
	})
	m.watchSelfTestsOnce(context.Background())

	select {
	case e := <-ch:
		if e.Type != SelfTestProgress {
			t.Errorf("event = %q, want %q", e.Type, SelfTestProgress)
		}
	default:
		t.Error("no event for a reading that changed; the badge would not move")
	}
}

// Nothing testing means nothing read. This is the property that keeps the watcher from being a second
// scan: it cannot wake a sleeping drive, because it only ever looks at drives already mid-test.
func TestTheWatcherReadsNothingWhenNothingIsTesting(t *testing.T) {
	st := watchFixture(t, false, nil, "")
	reads := 0

	m := watchManager(t, st, events.NewHub(), func() (smart.Result, error) {
		reads++
		return smart.Result{}, nil
	})
	m.watchSelfTestsOnce(context.Background())

	if reads != 0 {
		t.Errorf("%d drives were read with nothing testing; the watcher must idle", reads)
	}
}

// A drive that cannot be read is not a drive that finished. Clearing the badge on a failed read would
// hide a test that is still going, which is worse than a stale figure.
func TestAFailedReadLeavesTheStateAlone(t *testing.T) {
	pct := 42
	st := watchFixture(t, true, &pct, "Extended offline")

	m := watchManager(t, st, events.NewHub(), func() (smart.Result, error) {
		return smart.Result{}, context.DeadlineExceeded
	})
	m.watchSelfTestsOnce(context.Background())

	state, err := st.SelfTestState(context.Background(), "S")
	if err != nil {
		t.Fatalf("SelfTestState: %v", err)
	}
	if !state.Running || state.Pct == nil || *state.Pct != 42 {
		t.Errorf("state = %+v, want it untouched after a failed read", state)
	}
}
