package store

import (
	"context"
	"time"
)

// HealthAt returns each drive's verdict as of the newest snapshot at or before `before`.
//
// Used by the digest (`FEATURES.md` B13) to answer "what changed over the period" without storing a
// second history: the snapshots are already the history, and the verdict is on every one of them.
//
// A drive with no snapshot that old is simply absent — the caller treats a missing baseline as
// "we were not watching it yet" rather than as a change, which is the honest reading.
func (s *Store) HealthAt(ctx context.Context, before time.Time) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.serial, s.health
		FROM snapshots s
		JOIN (
			SELECT serial, MAX(id) AS id FROM snapshots WHERE ts <= ? GROUP BY serial
		) newest ON newest.id = s.id`, before.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var serial, health string
		if err := rows.Scan(&serial, &health); err != nil {
			return nil, err
		}
		out[serial] = health
	}
	return out, rows.Err()
}

// ScanCounts returns how many scans completed and how many failed in a period.
//
// Both figures matter for a digest: "7 completed" is the reassuring half and "1 failed" is the one
// that explains a gap in the history.
func (s *Store) ScanCounts(ctx context.Context, from, to time.Time) (ok, failed int, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN ok = 1 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN ok = 0 THEN 1 ELSE 0 END), 0)
		FROM scan_history WHERE ended_at >= ? AND ended_at <= ?`,
		from.UnixMilli(), to.UnixMilli()).Scan(&ok, &failed)
	return ok, failed, err
}
