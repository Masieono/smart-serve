package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// DigestStore is what a digest needs beyond the alert tunables: the fleet, its history and the
// period's scans.
//
// Separate from `Settings` rather than folded into it, so a caller that only *delivers* events — the
// dispatcher's tests, for instance — does not have to supply a database.
type DigestStore interface {
	Settings
	ListDevices(ctx context.Context) ([]store.DeviceRecord, error)
	LatestSnapshotFacts(ctx context.Context) (map[string]store.SnapshotFacts, error)
	HealthAt(ctx context.Context, before time.Time) (map[string]string, error)
	ScanCounts(ctx context.Context, from, to time.Time) (int, int, error)
	SectorTrend(ctx context.Context, from time.Time, recent time.Duration) (map[string]int64, error)
}

// DigestSender builds and dispatches a scheduled digest (`FEATURES.md` B13).
//
// It is a separate type from `Notifier` because the two answer different questions. `Notifier` reacts
// to a scan that just finished and says what *changed*; this one runs on a schedule and says what the
// fleet *is*. Sharing a type would mean one struct with two unrelated sets of dependencies.
type DigestSender struct {
	store DigestStore
	disp  *Dispatcher
	log   *slog.Logger
	// version and the tunables are read per send, like the notifier's, so a settings change takes
	// effect without a restart.
	version string
}

// NewDigestSender wires a digest sender to a store and a dispatcher.
func NewDigestSender(st DigestStore, disp *Dispatcher, version string, log *slog.Logger) *DigestSender {
	if log == nil {
		log = slog.Default()
	}
	return &DigestSender{store: st, disp: disp, log: log, version: version}
}

// SendDigest assembles one period's summary and dispatches it.
//
// The period is **since the schedule last ran**, which is what makes the message mean "since we last
// wrote to you" rather than "the last seven days" — a digest on a monthly schedule would otherwise
// repeat itself. A schedule that has never run falls back to seven days, so the first digest is not
// a summary of all history.
//
// It returns an error so the scheduler can log it, but a caller must not treat a failure as the
// schedule failing: the boundary arrived, and the schedule has to advance either way.
func (s *DigestSender) SendDigest(ctx context.Context, sched store.Schedule) error {
	now := time.Now()
	from := now.AddDate(0, 0, -7)
	if sched.LastRun != nil {
		from = *sched.LastRun
	}
	return s.SendPeriod(ctx, from, now)
}

// SendPeriod sends a digest for an explicit window, which is what "run now" uses and what the tests
// drive.
func (s *DigestSender) SendPeriod(ctx context.Context, from, to time.Time) error {
	opts := s.alertSettings(ctx)

	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return err
	}
	// Best-effort, like the dashboard: a missing figure drops a line from the digest rather than
	// failing the message. A digest that did not arrive is worse than one missing a count.
	facts, ferr := s.store.LatestSnapshotFacts(ctx)
	if ferr != nil {
		facts = nil
	}
	before, berr := s.store.HealthAt(ctx, from)
	if berr != nil {
		before = nil
	}
	worsening, werr := s.store.SectorTrend(ctx, from, to.Sub(from))
	if werr != nil {
		worsening = nil
	}
	scansOK, scansFailed, cerr := s.store.ScanCounts(ctx, from, to)
	if cerr != nil {
		s.log.Warn("could not count the period's scans", "err", cerr)
	}

	d := Digest{
		From: from, To: to,
		Devices: devices, Facts: facts, Before: before, Worsening: worsening,
		ScansOK: scansOK, ScansFailed: scansFailed,
		Ranking: digestRanking(devices, facts, worsening),
	}

	ev := DigestEvent(d, opts)
	if ev.Rationale == "" {
		return nil
	}
	s.disp.Dispatch(ctx, []Event{ev})
	return nil
}

// digestRanking builds the replace-first shortlist from stored records (`FEATURES.md` A24).
//
// The same mapping the dashboard makes, from the same two sources — the device row and the newest
// snapshot — so the two cannot rank a drive differently. The dashboard maps from its JSON payload
// rather than from the record, because that is what it has; the fields are the same either way.
func digestRanking(
	devices []store.DeviceRecord,
	facts map[string]store.SnapshotFacts,
	worsening map[string]int64,
) []interp.ReplacementRank {
	candidates := make([]interp.ReplacementCandidate, 0, len(devices))
	for _, d := range devices {
		// A disconnected drive is not a candidate, and neither is one on the bench: both are findings
		// of their own, and ranking them would put a drive nobody is using above one that is failing.
		if d.IsMissing || store.HasTag(d.Tags, store.BenchTag) {
			continue
		}
		var wear *float64
		if f, ok := facts[d.Serial]; ok {
			wear = f.WearPct
		}
		candidates = append(candidates, interp.ReplacementCandidate{
			Serial:    d.Serial,
			Name:      d.Name,
			Health:    model.Verdict(d.Health),
			Rationale: d.HealthRationale,
			Score:     d.Score,
			Worsening: worsening[d.Serial],
			WearPct:   wear,
		})
	}
	return interp.RankForReplacement(candidates)
}

// alertSettings reads the same tunables the notifier does, so a digest is worded in the same unit and
// carries the same link back into the app.
func (s *DigestSender) alertSettings(ctx context.Context) CollectOptions {
	opts := CollectOptions{TemperatureUnit: "c", Version: s.version}
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return opts
	}
	if unit := settings["temperature_unit"]; unit != "" {
		opts.TemperatureUnit = unit
	}
	opts.BaseURL = settings["public_url"]
	return opts
}
