// Package sched runs scheduled scans inside the process (ADR-004).
//
// It is a single goroutine that sleeps until the next due schedule — never a
// ticker — which is what keeps the idle-cost promise: with nothing due the
// process does no work at all until either the next boundary or a UI change
// nudges it (FEATURES.md §E).
package sched

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/selftest"
	"github.com/masieono/smart-serve/internal/store"
)

// Runner performs a scan on the scheduler's behalf. *scan.Manager satisfies it.
type Runner interface {
	Scan(ctx context.Context, t scan.Trigger) (scan.Result, bool, error)
}

// Digester sends a scheduled digest (`FEATURES.md` B13). *notify.DigestSender satisfies it.
//
// A schedule does two different things depending on its action, so the scheduler takes two
// dependencies rather than one interface with two methods: a caller that only runs scans should not
// have to supply a digest sender, and vice versa.
type Digester interface {
	SendDigest(ctx context.Context, sched store.Schedule) error
}

// SelfTester runs a scheduled self-test pass (`FEATURES.md` A19). *selftest.Runner satisfies it.
//
// A third dependency for the same reason as the second: the action decides what a fire means, and a
// caller that has no business writing to drives should not have to supply something that does.
type SelfTester interface {
	// StartQueue begins a pass and returns how many drives it queued. It returns 0 with a nil error
	// when there was nothing eligible, and `selftest.ErrAlreadyRunning` when a pass is draining.
	StartQueue(ctx context.Context, kind string) (int, error)
}

// retryInterval is how far a schedule is advanced when its next boundary cannot
// be computed. It keeps a malformed expression from spinning the loop.
const retryInterval = time.Minute

// Options configures a Scheduler.
type Options struct {
	// Location is the timezone cron expressions are evaluated in. Cron is a
	// wall-clock concept, so this matters (DECISIONS.md).
	Location *time.Location
	// MissedRunPolicy decides what happens to a schedule whose next run passed
	// while the process was down: "run_once" fires it once at startup, anything
	// else ("skip", the default) simply reschedules.
	MissedRunPolicy string
	// ScanTimeout bounds one scheduled scan (default 10m).
	ScanTimeout time.Duration
	// Digester sends a scheduled digest (`FEATURES.md` B13). Without it, a digest schedule logs that
	// it has nowhere to send and still advances, rather than failing or spinning.
	Digester Digester
	// SelfTester runs a scheduled self-test pass (`FEATURES.md` A19). Without it, a self-test schedule
	// logs that it has nowhere to run and still advances.
	SelfTester SelfTester
	Logger     *slog.Logger
	// Now overrides the clock in tests.
	Now func() time.Time
}

// Scheduler wakes at the next due schedule and runs it.
type Scheduler struct {
	store      *store.Store
	runner     Runner
	digester   Digester
	selfTester SelfTester
	hub        *events.Hub
	log        *slog.Logger

	loc             *time.Location
	missedRunPolicy string
	scanTimeout     time.Duration
	now             func() time.Time

	wake chan struct{}
}

// New builds a scheduler.
func New(st *store.Store, r Runner, hub *events.Hub, opts Options) *Scheduler {
	s := &Scheduler{
		store: st, runner: r, digester: opts.Digester, selfTester: opts.SelfTester, hub: hub, log: opts.Logger,
		loc: opts.Location, missedRunPolicy: opts.MissedRunPolicy,
		scanTimeout: opts.ScanTimeout, now: opts.Now,
		wake: make(chan struct{}, 1),
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.loc == nil {
		s.loc = time.Local
	}
	if s.scanTimeout <= 0 {
		s.scanTimeout = 10 * time.Minute
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Wake tells the loop to re-evaluate schedules now. It never blocks: a wake-up
// already pending is as good as a new one.
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// RunNow makes a schedule due immediately and wakes the loop. This is how "run
// now" works: the run is recorded by the same path as any other fire, so
// last-run and next-run stay honest.
func (s *Scheduler) RunNow(ctx context.Context, scheduleID int64) error {
	if err := s.store.MakeScheduleDue(ctx, scheduleID, s.now()); err != nil {
		return err
	}
	s.Wake()
	return nil
}

// Run drives the loop until ctx is cancelled. Start it once, in its own
// goroutine.
func (s *Scheduler) Run(ctx context.Context) {
	s.log.Info("scheduler started", "timezone", s.resolveLocation(ctx).String())
	s.reconcileStartup(ctx)
	for {
		next, id, ok := s.nextDue(ctx)
		if !ok {
			// Nothing enabled: wait for a change. No polling, no timer.
			if !s.sleepUntil(ctx, nil) {
				return
			}
			continue
		}
		if !s.sleepUntil(ctx, &next) {
			return
		}
		s.fire(ctx, id)
	}
}

// resolveLocation returns the zone cron is evaluated in: the `timezone` setting
// when set, otherwise the container's own zone (DECISIONS.md — "load the container
// TZ; evaluate cron in local time").
func (s *Scheduler) resolveLocation(ctx context.Context) *time.Location {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return s.loc
	}
	name := strings.TrimSpace(settings["timezone"])
	if name == "" {
		return s.loc
	}
	loc, lerr := time.LoadLocation(name)
	if lerr != nil {
		s.log.Warn("unknown timezone setting; falling back to the container zone",
			"timezone", name, "err", lerr)
		return s.loc
	}
	return loc
}

// resolveMissedRunPolicy returns the policy for overdue schedules: the setting when
// present, otherwise the value seeded from the environment.
func (s *Scheduler) resolveMissedRunPolicy(ctx context.Context) string {
	if settings, err := s.store.GetSettings(ctx); err == nil {
		if v := strings.ToLower(strings.TrimSpace(settings["missed_run_policy"])); v != "" {
			return v
		}
	}
	return s.missedRunPolicy
}

// reconcileStartup applies the missed-run policy once, at start-up.
//
// This is the only place the policy belongs. A boundary that passed while the
// process was down is either rescheduled (the default) or left overdue so it
// fires exactly once. Afterwards a past boundary simply means "due now" — which
// is what makes "run now" and a boundary arriving on time both work.
func (s *Scheduler) reconcileStartup(ctx context.Context) {
	if s.resolveMissedRunPolicy(ctx) == "run_once" {
		return
	}
	schedules, err := s.store.ListSchedules(ctx)
	if err != nil {
		s.log.Error("scheduler could not list schedules", "err", err)
		return
	}
	now := s.now()
	loc := s.resolveLocation(ctx)
	for _, sched := range schedules {
		if !sched.Enabled || sched.NextRun == nil || sched.NextRun.After(now) {
			continue
		}
		next, nerr := s.nextAfter(sched, now, loc)
		if nerr != nil {
			s.log.Error("scheduler could not reschedule a missed run", "schedule", sched.ID, "err", nerr)
			continue
		}
		s.log.Info("skipping a missed run", "schedule", sched.ID, "was_due", *sched.NextRun, "next_run", next)
		if serr := s.store.SetScheduleNextRun(ctx, sched.ID, next); serr != nil {
			s.log.Error("scheduler could not reschedule a missed run", "schedule", sched.ID, "err", serr)
		}
	}
}

// sleepUntil blocks until the deadline, a wake-up, or cancellation. A nil
// deadline waits indefinitely. It reports false when ctx was cancelled.
func (s *Scheduler) sleepUntil(ctx context.Context, deadline *time.Time) bool {
	var (
		timer *time.Timer
		ch    <-chan time.Time
	)
	if deadline != nil {
		wait := deadline.Sub(s.now())
		if wait <= 0 {
			return true // already due
		}
		timer = time.NewTimer(wait)
		ch = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	select {
	case <-ctx.Done():
		return false
	case <-s.wake:
		return true
	case <-ch:
		return true
	}
}

// nextDue returns the earliest fire time among enabled schedules, computing and
// persisting any schedule whose next run was never set or has fallen into the
// past.
func (s *Scheduler) nextDue(ctx context.Context) (time.Time, int64, bool) {
	schedules, err := s.store.ListSchedules(ctx)
	if err != nil {
		s.log.Error("scheduler could not list schedules", "err", err)
		return time.Time{}, 0, false
	}

	loc := s.resolveLocation(ctx)
	var (
		best   time.Time
		bestID int64
	)
	for _, sched := range schedules {
		if !sched.Enabled {
			continue
		}
		next, err := s.boundary(ctx, sched, loc)
		if err != nil {
			s.log.Error("scheduler could not compute the next run", "schedule", sched.ID, "err", err)
			continue
		}
		if bestID == 0 || next.Before(best) {
			best, bestID = next, sched.ID
		}
	}
	return best, bestID, bestID != 0
}

// boundary resolves when a schedule should next fire.
//
// A schedule with no boundary yet gets one computed and stored. A boundary that
// is already past is returned as-is: by this point the only reasons for that are
// that it just came due, or that the operator pressed "run now" — both of which
// mean "run it now". Missed runs were already reconciled at start-up.
func (s *Scheduler) boundary(ctx context.Context, sched store.Schedule, loc *time.Location) (time.Time, error) {
	if sched.NextRun != nil {
		return *sched.NextRun, nil
	}
	next, err := s.nextAfter(sched, s.now(), loc)
	if err != nil {
		return time.Time{}, err
	}
	if err := s.store.SetScheduleNextRun(ctx, sched.ID, next); err != nil {
		return time.Time{}, err
	}
	s.log.Info("scheduler armed", "schedule", sched.ID, "next_run", next)
	return next, nil
}

// nextAfter computes the first fire time strictly after `from`.
func (s *Scheduler) nextAfter(sched store.Schedule, from time.Time, loc *time.Location) (time.Time, error) {
	if sched.Kind == "interval" {
		if sched.EverySeconds == nil || *sched.EverySeconds <= 0 {
			return time.Time{}, fmt.Errorf("interval schedule %d has no interval", sched.ID)
		}
		return from.Add(time.Duration(*sched.EverySeconds) * time.Second), nil
	}
	return Next(sched.Cron, from, loc)
}

// sendDigest sends one period's summary (`FEATURES.md` B13).
//
// A failure is logged rather than propagated: the schedule's boundary arrived, so the bookkeeping
// that advances it has to happen either way. That is the same rule the scan path follows — a broken
// scanner must not stop the schedule, and neither must a broken notification channel.
func (s *Scheduler) sendDigest(ctx context.Context, sched store.Schedule) {
	if s.digester == nil {
		s.log.Warn("digest schedule has nowhere to send", "schedule", sched.ID)
		return
	}
	if err := s.digester.SendDigest(ctx, sched); err != nil {
		s.log.Warn("digest failed", "schedule", sched.ID, "err", err)
	}
}

// startSelfTest begins a pass over every drive that can take one (`FEATURES.md` A19, `ADR-024`).
//
// Queued rather than run here, because a pass takes hours and the scheduler is one goroutine every
// other schedule shares. A fire that arrives while a pass is still draining is logged and skipped:
// two overlapping passes would test the same drives twice, and the second one would find each drive
// already testing itself and skip it anyway — so the honest thing is to say so up front.
func (s *Scheduler) startSelfTest(ctx context.Context, sched store.Schedule) {
	if s.selfTester == nil {
		s.log.Warn("self-test schedule has nowhere to run", "schedule", sched.ID)
		return
	}
	queued, err := s.selfTester.StartQueue(ctx, sched.SelfTestKind)
	switch {
	case errors.Is(err, selftest.ErrAlreadyRunning):
		s.log.Info("self-test schedule skipped: a pass is still running", "schedule", sched.ID)
	case err != nil:
		s.log.Warn("self-test schedule could not start", "schedule", sched.ID, "err", err)
	case queued == 0:
		s.log.Info("self-test schedule found no drive it could test", "schedule", sched.ID)
	default:
		s.log.Info("self-test pass queued", "schedule", sched.ID, "drives", queued,
			"kind", sched.SelfTestKind)
	}
}

// fire runs one scheduled action and advances the schedule. The run is recorded
// even when the scan fails, so a broken scanner keeps firing on its interval —
// and, per the event catalog, keeps saying so.
func (s *Scheduler) fire(ctx context.Context, id int64) {
	// The action decides what a fire *means* (`FEATURES.md` B13), so it is read before anything
	// happens: a digest is not a scan, and the difference has to be known up front.
	sched, err := s.store.GetSchedule(ctx, id)
	if err != nil {
		s.log.Error("could not read the schedule before running it", "schedule", id, "err", err)
		return
	}
	if sched == nil {
		return // deleted between the wake and the fire
	}

	s.log.Info("running scheduled action", "schedule", id, "action", sched.Action)
	s.hub.Publish("schedule_fired", map[string]any{"schedule_id": id})

	// A self-test pass is *not* bounded by the scan timeout: it runs for as long as the drives take,
	// which is hours, and it has its own queue and its own reason to stop (`ADR-024`). Everything
	// else must not be cancelled by an unrelated request context, but is still bounded.
	var res scan.Result
	switch {
	case sched.Action == store.ActionSelfTest:
		s.startSelfTest(ctx, *sched)
	case sched.Action == store.ActionDigest:
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.scanTimeout)
		defer cancel()
		s.sendDigest(runCtx, *sched)
	default:
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.scanTimeout)
		defer cancel()
		var rerr error
		res, _, rerr = s.runner.Scan(runCtx, scan.Trigger{Kind: "schedule", ScheduleID: id})
		if rerr != nil {
			s.log.Warn("scheduled scan failed", "schedule", id, "err", rerr)
		}
	}

	// Bookkeeping survives cancellation: a run that happened must be recorded,
	// and the schedule must always move forward so the loop cannot spin on it.
	writeCtx := context.WithoutCancel(ctx)
	lastRun := s.now()
	next := lastRun.Add(retryInterval)

	sched, gerr := s.store.GetSchedule(writeCtx, id)
	switch {
	case gerr != nil:
		s.log.Error("could not read the schedule after its run", "schedule", id, "err", gerr)
	case sched == nil:
		return // deleted while the scan was running; nothing left to advance
	default:
		computed, cerr := s.nextAfter(*sched, lastRun, s.resolveLocation(writeCtx))
		if cerr != nil {
			s.log.Error("could not compute the next run; retrying later", "schedule", id, "err", cerr)
		} else {
			next = computed
		}
	}
	if merr := s.store.MarkScheduleRun(writeCtx, id, lastRun, next); merr != nil {
		s.log.Error("could not record the schedule run", "schedule", id, "err", merr)
	}

	s.hub.Publish("schedule_ran", map[string]any{
		"schedule_id": id,
		"scan_id":     res.ScanID,
		"ok":          res.OK,
		"next_run":    next,
	})
}
