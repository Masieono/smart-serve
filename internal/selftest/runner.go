// Package selftest runs scheduled self-tests, one drive at a time (`FEATURES.md` A19, `ADR-024`).
//
// A self-test is the only thing this app does that writes to a drive, and a *scheduled* one is the
// only path by which that happens without somebody pressing a button. The button's own constraints
// are unchanged — opt-in, never automatic, abortable — and a schedule adds two of its own:
//
//   - **One drive at a time.** Twenty-four long tests starting together is an I/O meltdown, and the
//     queue exists for that and nothing else.
//   - **A defined order.** Sorted by serial: stable, unique, and not something the user can edit, so
//     renaming a drive or moving it to another bay cannot reorder a queue that is already draining.
//
// A pass takes hours, so `StartQueue` returns as soon as it is running and the work happens in its
// own goroutine. The scheduler that starts it is never blocked behind it.
package selftest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// ErrAlreadyRunning is returned when a pass is already draining. The caller logs it and skips the
// fire, rather than queueing a second pass over the same drives.
var ErrAlreadyRunning = errors.New("a self-test pass is already running")

// PollInterval is how often the runner asks the drive whether it is still testing.
//
// A minute, and that is not a compromise: the drive is mid-test, so one extra `smartctl` read a
// minute is nothing beside the test's own I/O, and the read is exact rather than estimated — which
// is what lets the next drive start the moment the previous one finishes. This is inside the single
// documented exception to "no periodic device reads" (`FEATURES.md` §E): a test this app started is
// running, and its progress exists nowhere the app stores.
const PollInterval = time.Minute

// minWaitFloor is how long the runner waits before it will believe "no test running" from a drive
// that never reported one starting.
//
// Some drives write no log entry until a test *finishes*, which is the same blind spot the self-test
// card has. Without a floor, the first poll would conclude the drive was already done and start the
// next one on top of a test that is still running.
const minWaitFloor = 2 * time.Minute

// minWaitCeiling bounds the drive's own estimate. A drive reporting an implausible estimate must not
// be able to stall a pass for a day.
const minWaitCeiling = 8 * time.Hour

// Driver is the two smartctl operations a pass needs.
//
// Function fields rather than an interface, because the production wiring is two lines and a test's
// fake is a struct literal — and because these are package-level functions in `smart`, so there is
// nothing to hang a method on.
type Driver struct {
	Start func(ctx context.Context, timeout time.Duration, path, transport string, kind smart.SelfTestKind) (smart.Result, error)
	Read  func(ctx context.Context, timeout time.Duration, path, transport string) (smart.Result, error)
}

// Store is the slice of the store a pass reads.
type Store interface {
	ListDevices(ctx context.Context) ([]store.DeviceRecord, error)
}

// Rescanner re-reads one drive after its test finishes.
//
// This is how a finished result reaches the user: the scan stores it, and the notification path that
// already exists (`B9`) sees a new result where there was none. Notifying from here instead would
// mean a second copy of that comparison, and the two would drift.
type Rescanner interface {
	ScanDevice(ctx context.Context, serial, path string, t scan.Trigger) (scan.Result, bool, error)
}

// Options configures a Runner.
type Options struct {
	Store Store
	// Driver is the smartctl operations. Required.
	Driver Driver
	// Rescanner re-reads a drive after its test. Optional: without it the result is still on the
	// drive and arrives at the next scan, which is the honest fallback rather than a failure.
	Rescanner Rescanner
	Logger    *slog.Logger
	// PollInterval overrides how often the drive is asked (default `PollInterval`).
	PollInterval time.Duration
	// CommandTimeout supplies the per-command timeout. Required.
	CommandTimeout func(ctx context.Context) time.Duration
}

// Runner runs self-tests one drive at a time.
type Runner struct {
	store   Store
	driver  Driver
	rescan  Rescanner
	log     *slog.Logger
	poll    time.Duration
	timeout func(ctx context.Context) time.Duration
	now     func() time.Time

	mu      sync.Mutex
	running bool
	done    int
	total   int
	current string
	cancel  context.CancelFunc
}

// New builds a runner.
func New(opts Options) *Runner {
	r := &Runner{
		store: opts.Store, driver: opts.Driver, rescan: opts.Rescanner, log: opts.Logger,
		poll: opts.PollInterval, timeout: opts.CommandTimeout, now: time.Now,
	}
	if r.log == nil {
		r.log = slog.Default()
	}
	if r.poll <= 0 {
		r.poll = PollInterval
	}
	if r.timeout == nil {
		r.timeout = func(context.Context) time.Duration { return smart.DefaultTimeout }
	}
	return r
}

// Progress is what a running pass is doing: how many drives it has finished, how many it queued, and
// the serial it is on.
type Progress struct {
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Current string `json:"current,omitempty"`
}

// Progress returns the state of the pass, for the schedules page to show. A pass runs for hours and
// changes a drive, so "it fired" is not enough for a reader to know what happened.
func (r *Runner) Progress() Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Progress{Running: r.running, Done: r.done, Total: r.total, Current: r.current}
}

// StartQueue begins a pass over every eligible drive and returns as soon as it is running. The count
// is how many drives were queued.
//
// It returns immediately because a pass takes hours: a long test on a 14 TB drive runs for most of a
// day. The pass outlives the context that started it in the sense that a fire has no deadline of its
// own, but not in the sense of surviving shutdown — cancellation stops the pass, and the drive keeps
// testing. Aborting the drives would throw away hours of work that is still doing exactly what it
// was asked to do.
func (r *Runner) StartQueue(parent context.Context, kind string) (int, error) {
	kind = normaliseKind(kind)

	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return 0, ErrAlreadyRunning
	}
	r.mu.Unlock()

	devices, err := Eligible(parent, r.store)
	if err != nil {
		return 0, err
	}
	if len(devices) == 0 {
		return 0, nil
	}

	// Derived from the caller's context, *not* detached from it: the pass must stop when the process
	// does. What it must not be is bounded by a scan's timeout — a pass runs for hours — and that is
	// the scheduler's job, not this one's, because the scheduler is what knows the difference.
	ctx, cancel := context.WithCancel(parent)

	r.mu.Lock()
	r.running, r.done, r.total, r.current, r.cancel = true, 0, len(devices), "", cancel
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			r.running, r.current, r.cancel = false, "", nil
			r.mu.Unlock()
			cancel()
		}()
		r.run(ctx, kind, devices)
	}()
	return len(devices), nil
}

// Stop cancels a running pass. The drives that have not started are skipped; the one testing
// continues on the drive.
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Eligible returns the drives a pass would test, in the order it would test them.
//
// Exported so a caller can answer "what would this schedule do" without starting anything, and so
// the skip rules live in one place.
func Eligible(ctx context.Context, st Store) ([]store.DeviceRecord, error) {
	devices, err := st.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]store.DeviceRecord, 0, len(devices))
	for _, rec := range devices {
		if WhyNot(rec) != "" {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Serial < out[j].Serial })
	return out, nil
}

// WhyNot reports why a drive cannot take a scheduled self-test, or "" when it can.
//
// Read from what the last scan stored rather than by asking the drive: a pass should not read every
// drive in the fleet just to decide which ones to read.
func WhyNot(rec store.DeviceRecord) string {
	if strings.TrimSpace(rec.DevicePath) == "" {
		return "no device path is recorded"
	}
	if !rec.SmartAvailable {
		// Covers a drive whose S.M.A.R.T. could not be read *and* one that reports none at all:
		// neither can be asked to test itself.
		return "S.M.A.R.T. could not be read"
	}
	// The drive's own answer: a drive that says it does not implement self-tests is skipped rather
	// than asked and refused.
	summary := smart.ParseLogs([]byte(rec.RawOutput))
	if summary.SelfTestSupported != nil && !*summary.SelfTestSupported {
		return "the drive does not implement self-tests"
	}
	return ""
}

func (r *Runner) run(ctx context.Context, kind string, devices []store.DeviceRecord) {
	r.log.Info("scheduled self-test pass started", "drives", len(devices), "kind", kind)
	started := 0
	for _, rec := range devices {
		if ctx.Err() != nil {
			r.log.Info("scheduled self-test pass stopped", "started", started, "queued", len(devices))
			return
		}
		r.setCurrent(rec.Serial)
		ok, err := r.testOne(ctx, rec, kind)
		switch {
		case err != nil:
			r.log.Warn("scheduled self-test could not run", "serial", rec.Serial, "err", err)
		case ok:
			started++
		}
		r.bumpDone()
	}
	r.log.Info("scheduled self-test pass finished", "started", started, "queued", len(devices))
}

// testOne runs one drive's test and waits for it to end, reporting whether a test was started.
func (r *Runner) testOne(ctx context.Context, rec store.DeviceRecord, kind string) (bool, error) {
	timeout := r.timeout(ctx)

	// Read before writing. A test already in progress — started by hand, or left over from a pass
	// that was interrupted — is not restarted: the drive refuses a second one, and on NVMe that
	// refusal arrives as a bare exit status, which is exactly the "every test after the first fails"
	// report from the field.
	before, err := r.driver.Read(ctx, timeout, rec.DevicePath, rec.TransportOverride)
	if err != nil {
		return false, fmt.Errorf("read self-test log: %w", err)
	}
	summary := smart.ParseLogs(before.Stdout)
	if summary.Running() != nil {
		r.log.Info("skipping a drive that is already testing itself", "serial", rec.Serial)
		return false, nil
	}

	res, err := r.driver.Start(ctx, timeout, rec.DevicePath, rec.TransportOverride, smart.SelfTestKind(kind))
	if err != nil {
		return false, err
	}
	r.log.Info("scheduled self-test started", "serial", rec.Serial, "kind", kind,
		"note", smart.Messages(res.Stdout, res.Stderr))

	r.wait(ctx, rec, kind, summary)
	return true, nil
}

// wait blocks until the drive reports no test running, or the pass is cancelled.
//
// The drive's own estimate sets a floor before "no test running" can be believed, because some drives
// write nothing to the log until a test *finishes* — the same blind spot the self-test card has. A
// poll that reports the test running is authoritative and extends the wait indefinitely: a running
// test is evidence, and a clock is not.
func (r *Runner) wait(ctx context.Context, rec store.DeviceRecord, kind string, before model.LogSummary) {
	timeout := r.timeout(ctx)
	started := r.now()
	floor := estimateFloor(before, kind)
	reportedRunning := false

	for {
		select {
		case <-ctx.Done():
			// The drive keeps testing. Stopping the pass is not the same as stopping the drive, and
			// it should not be: hours of work that is doing the right thing is not waste.
			r.log.Info("leaving a self-test running on the drive", "serial", rec.Serial)
			return
		case <-time.After(r.poll):
		}

		res, err := r.driver.Read(ctx, timeout, rec.DevicePath, rec.TransportOverride)
		if err != nil {
			// A read failure is not a finished test. Keep asking: the drive is mid-test, and
			// treating this as "done" would start the next drive on top of it.
			r.log.Warn("could not read self-test progress; still waiting", "serial", rec.Serial, "err", err)
			continue
		}
		after := smart.ParseLogs(res.Stdout)
		if running := after.Running(); running != nil {
			reportedRunning = true
			continue
		}
		if !reportedRunning && r.now().Sub(started) < floor {
			continue // too early to believe it, and it never said it had started
		}
		r.finish(ctx, rec, after)
		return
	}
}

// finish re-reads the drive so the result is stored and notified by the path a manual scan uses.
func (r *Runner) finish(ctx context.Context, rec store.DeviceRecord, after model.LogSummary) {
	entry := after.LatestFinished()
	switch {
	case entry == nil:
		r.log.Warn("self-test ended with no result in the drive's log", "serial", rec.Serial)
	case entry.Passed != nil && !*entry.Passed:
		r.log.Warn("scheduled self-test FAILED", "serial", rec.Serial, "type", entry.Type,
			"status", entry.Status)
	default:
		r.log.Info("scheduled self-test finished", "serial", rec.Serial, "type", entry.Type,
			"status", entry.Status)
	}
	if r.rescan == nil {
		return
	}
	// The rescans are serialised by the queue itself, which is the point: the fleet is never re-read
	// while a test is running on it.
	if _, _, err := r.rescan.ScanDevice(ctx, rec.Serial, rec.DevicePath,
		scan.Trigger{Kind: "selftest"}); err != nil {
		r.log.Warn("could not re-read a drive after its self-test", "serial", rec.Serial, "err", err)
	}
}

// estimateFloor is how long to wait before believing a drive that never reported its test starting.
//
// The drive's own estimate for that kind of test when it gave one, clamped; the floor otherwise.
func estimateFloor(before model.LogSummary, kind string) time.Duration {
	var minutes *int
	if kind == store.SelfTestLong {
		minutes = before.ExtendedMinutes
	} else {
		minutes = before.ShortMinutes
	}
	if minutes == nil || *minutes <= 0 {
		return minWaitFloor
	}
	d := time.Duration(*minutes) * time.Minute
	if d < minWaitFloor {
		return minWaitFloor
	}
	if d > minWaitCeiling {
		return minWaitCeiling
	}
	return d
}

// normaliseKind maps anything unrecognised to `short`.
//
// The same rule the store, the API and the UI apply, and it guesses cheap on purpose: a mistyped
// value that silently meant *long* would turn a schedule into hours of I/O on every drive.
func normaliseKind(kind string) string {
	if strings.EqualFold(strings.TrimSpace(kind), store.SelfTestLong) {
		return store.SelfTestLong
	}
	return store.SelfTestShort
}

func (r *Runner) setCurrent(serial string) {
	r.mu.Lock()
	r.current = serial
	r.mu.Unlock()
}

func (r *Runner) bumpDone() {
	r.mu.Lock()
	r.done++
	r.mu.Unlock()
}
