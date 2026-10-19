package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// --- settings -----------------------------------------------------------------

// GetSettings returns every setting as a key/value map.
func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// GetSetting returns one setting and whether it was present.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting upserts one setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetSettings upserts several settings atomically.
func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return fmt.Errorf("set setting %q: %w", k, err)
		}
	}
	return tx.Commit()
}

// --- retention -----------------------------------------------------------------

// DefaultRetentionDays is how long snapshots are kept when nothing says otherwise.
//
// It is the default for the `retention_days` setting, and the one place it is written down: the
// scan's pruning and every question about history read it from here, so the two cannot disagree
// about how much history exists.
const DefaultRetentionDays = 180

// DefaultLogRetentionDays is the same for the notification log.
const DefaultLogRetentionDays = 30

// RetentionDays is how long this deployment keeps snapshots.
//
// A missing, unparseable or non-positive setting falls back to the default rather than meaning
// "keep nothing": pruning the whole history would be a catastrophic reading of a typo, and the
// settings UI cannot produce a zero, so a zero means something went wrong.
func (s *Store) RetentionDays(ctx context.Context) int {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return DefaultRetentionDays
	}
	if days, ok := positiveSetting(settings, "retention_days"); ok {
		return days
	}
	return DefaultRetentionDays
}

// RetentionWindow is `RetentionDays` as a duration, for callers asking how far back history goes.
//
// This bounds every question about history — the longevity projection, the worsening flag, the
// trend adjustment — because snapshots older than it have been pruned. A longer window would be a
// promise the database cannot keep; a *shorter* one would hide history the user deliberately kept,
// which is exactly what `A26` set out to stop.
func (s *Store) RetentionWindow(ctx context.Context) time.Duration {
	return time.Duration(s.RetentionDays(ctx)) * 24 * time.Hour
}

// positiveSetting reads a positive integer setting, reporting whether it found a usable one.
func positiveSetting(settings map[string]string, key string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(settings[key]))
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// --- schedules ----------------------------------------------------------------

// Schedule is a configured recurring scan.
type Schedule struct {
	ID           int64
	Enabled      bool
	Kind         string // interval | cron
	EverySeconds *int64
	Cron         string
	Label        string
	LastRun      *time.Time
	NextRun      *time.Time
	CreatedAt    time.Time
}

// ListSchedules returns every schedule, oldest first.
func (s *Store) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, enabled, kind, every_seconds, cron, label, last_run, next_run, created_at
		FROM scan_schedules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Schedule, 0, 4)
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sched)
	}
	return out, rows.Err()
}

// GetSchedule returns one schedule, or nil when it does not exist.
func (s *Store) GetSchedule(ctx context.Context, id int64) (*Schedule, error) {
	sched, err := scanSchedule(s.db.QueryRowContext(ctx, `
		SELECT id, enabled, kind, every_seconds, cron, label, last_run, next_run, created_at
		FROM scan_schedules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sched, nil
}

// CreateSchedule inserts a schedule and returns its id.
func (s *Store) CreateSchedule(ctx context.Context, sched Schedule) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO scan_schedules
			(enabled, kind, every_seconds, cron, label, last_run, next_run, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		boolInt(sched.Enabled), sched.Kind, nullInt64(sched.EverySeconds), sched.Cron,
		sched.Label, ptrMS(sched.LastRun), ptrMS(sched.NextRun), time.Now().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("create schedule: %w", err)
	}
	return res.LastInsertId()
}

// UpdateSchedule replaces a schedule's mutable fields.
func (s *Store) UpdateSchedule(ctx context.Context, sched Schedule) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE scan_schedules
		SET enabled = ?, kind = ?, every_seconds = ?, cron = ?, label = ?,
		    last_run = ?, next_run = ?
		WHERE id = ?`,
		boolInt(sched.Enabled), sched.Kind, nullInt64(sched.EverySeconds), sched.Cron,
		sched.Label, ptrMS(sched.LastRun), ptrMS(sched.NextRun), sched.ID)
	if err != nil {
		return fmt.Errorf("update schedule %d: %w", sched.ID, err)
	}
	return nil
}

// DeleteSchedule removes a schedule.
func (s *Store) DeleteSchedule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scan_schedules WHERE id = ?`, id)
	return err
}

// SetScheduleNextRun records the next fire time without touching last_run. The
// scheduler uses it to persist a freshly computed boundary.
func (s *Store) SetScheduleNextRun(ctx context.Context, id int64, next time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE scan_schedules SET next_run = ? WHERE id = ?`, next.UnixMilli(), id)
	return err
}

// MarkScheduleRun records a completed run and its next fire time.
func (s *Store) MarkScheduleRun(ctx context.Context, id int64, lastRun, nextRun time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE scan_schedules SET last_run = ?, next_run = ? WHERE id = ?`,
		lastRun.UnixMilli(), nextRun.UnixMilli(), id)
	return err
}

// MakeScheduleDue brings a schedule's next run forward to now so the scheduler
// picks it up on its next wake — the mechanism behind "run now".
func (s *Store) MakeScheduleDue(ctx context.Context, id int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE scan_schedules SET next_run = ? WHERE id = ? AND enabled = 1`,
		now.UnixMilli(), id)
	return err
}

func scanSchedule(r rowScanner) (Schedule, error) {
	var (
		sched            Schedule
		enabled          int64
		every            sql.NullInt64
		lastRun, nextRun sql.NullInt64
		createdAt        int64
	)
	err := r.Scan(&sched.ID, &enabled, &sched.Kind, &every, &sched.Cron, &sched.Label,
		&lastRun, &nextRun, &createdAt)
	if err != nil {
		return sched, err
	}
	sched.Enabled = enabled != 0
	if every.Valid {
		v := every.Int64
		sched.EverySeconds = &v
	}
	sched.LastRun = timePtr(lastRun)
	sched.NextRun = timePtr(nextRun)
	sched.CreatedAt = fromMS(createdAt)
	return sched, nil
}
