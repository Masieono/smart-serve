package scan

import (
	"context"
	"time"

	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// SelfTestProgress is the event published when a drive's in-progress self-test changes
// (`FEATURES.md` A20). Open pages re-render on it, which is what keeps the fleet badge and the drive
// page's own progress showing the same reading.
const SelfTestProgress = "selftest_progress"

// WatchSelfTests keeps the recorded self-test state current while any drive is testing itself.
//
// The fleet badge and the drive page's progress both come from the *stored* row, which a scan writes.
// Start a long test, look at another drive, and nothing updated it until the next scheduled scan —
// which on a quiet homelab could be tomorrow. That was the report: "the badge didn't update, it
// essentially said testing 1% all the way until the test was complete and logged".
//
// Three things make this safe rather than a second scan:
//
//   - **Bounded by definition.** It only touches drives already mid-test, so it cannot wake a sleeping
//     one — the concern that governs every other decision about reading drives here.
//   - **One question per drive.** It reads the self-test document and writes three columns rather than
//     a whole row, so a scan happening at the same time is not fought.
//   - **It stops by itself.** When the last test ends the query returns nothing and the loop idles
//     without touching a drive. Nothing has to be cancelled or unwound.
func (m *Manager) WatchSelfTests(ctx context.Context) {
	// A tick short enough to notice a test starting. The *read* interval is the setting; this only
	// decides how soon after starting one the watcher picks it up, and a database query is free.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if time.Since(last) < m.store.SelfTestPollSeconds(ctx) {
				continue
			}
			last = time.Now()
			m.watchSelfTestsOnce(ctx)
		}
	}
}

// watchSelfTestsOnce re-reads every drive recorded as testing, and publishes what changed.
func (m *Manager) watchSelfTestsOnce(ctx context.Context) {
	// A scan in flight is already reading every drive and writing the state itself. Reading them
	// again on top of it would be two commands at once to the same drive, which is the one thing the
	// single-flight rule exists to prevent.
	if m.Running() {
		return
	}

	devices, err := m.store.TestingDevices(ctx)
	if err != nil || len(devices) == 0 {
		return
	}

	for _, d := range devices {
		if ctx.Err() != nil {
			return
		}
		res, err := m.selfTestReader(ctx, d.Path, d.TransportOverride)
		if err != nil {
			// The drive stopped answering, or the bridge dropped it. The recorded state is left
			// alone: a drive that cannot be read is not a drive that finished, and clearing the badge
			// on a failed read would hide a test that is still going.
			continue
		}

		running := smart.ParseLogs(res.Stdout).Running()
		next := store.SelfTestState{
			Running: running != nil,
			Pct:     runningPercent(running),
			Kind:    runningKind(running),
		}

		// Only news is worth publishing. A page re-rendered every fifteen seconds for an unchanged
		// reading would take the fleet's search box out from under whoever was typing in it.
		current, err := m.store.SelfTestState(ctx, d.Serial)
		if err != nil || sameSelfTestState(current, next) {
			continue
		}
		if err := m.store.SetSelfTestState(ctx, d.Serial, next.Running, next.Pct, next.Kind); err != nil {
			continue
		}
		m.hub.Publish(SelfTestProgress, map[string]any{
			"serial":  d.Serial,
			"running": next.Running,
			"pct":     next.Pct,
			"kind":    next.Kind,
		})
	}
}

// sameSelfTestState reports whether two readings say the same thing.
func sameSelfTestState(a, b store.SelfTestState) bool {
	if a.Running != b.Running || a.Kind != b.Kind {
		return false
	}
	if (a.Pct == nil) != (b.Pct == nil) {
		return false
	}
	return a.Pct == nil || *a.Pct == *b.Pct
}

// runningPercent is the drive's own figure, or nil when it does not report one.
//
// Nil rather than zero: many drives only write a log entry when a test *finishes*, so "running, no
// number" is a real state, and 0 would claim a test at its very beginning.
func runningPercent(entry *model.SelfTestEntry) *int {
	if entry == nil || entry.Remaining == nil {
		return nil
	}
	pct := 100 - *entry.Remaining
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return &pct
}

// runningKind is the drive's own words for the test, or empty when there is none.
func runningKind(entry *model.SelfTestEntry) string {
	if entry == nil {
		return ""
	}
	return entry.Type
}
