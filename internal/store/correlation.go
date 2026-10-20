package store

import (
	"context"
	"database/sql"
	"time"
)

// CorrelationMaxGap bounds how old a drive's "previous" reading may be (`FEATURES.md` A25).
//
// The signal correlated detection looks for is a change *between readings*: a fan stops and the next
// scan finds every drive in the chassis hotter. A rise measured across a long gap is a different
// thing entirely — a season turning, or a drive that spent three months in a drawer and is now in a
// warmer slot — so a reading older than this is not evidence of a change, it is evidence of a gap.
// Twenty-four hours covers every documented scan schedule with room to spare, and a gap is a better
// proxy for "nothing happened in between" than a scan boundary, which a single-drive rescan moves.
const CorrelationMaxGap = 24 * time.Hour

// TemperaturePair is one drive's temperature now and at its previous reading.
type TemperaturePair struct {
	Serial string
	Tags   string
	Prev   *int
	Now    *int
}

// TemperaturePairs returns each drive's two most recent temperatures.
//
// The pairing is per drive rather than per scan: a single-drive rescan writes one snapshot, so "the
// previous scan" would otherwise mean a different thing for every drive.
//
// A drive whose latest reading has no temperature is left out entirely — it cannot have moved, and
// including it would only shrink a group's ratio for a drive that reports nothing. A drive with a
// single reading is included with `Prev` nil: it belongs to its group, but it cannot be one of the
// drives that moved.
func (s *Store) TemperaturePairs(ctx context.Context) ([]TemperaturePair, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT serial, id, ts, temperature_c,
			       ROW_NUMBER() OVER (PARTITION BY serial ORDER BY id DESC) AS rn
			FROM snapshots
			WHERE temperature_c IS NOT NULL
		)
		SELECT nw.serial, COALESCE(dn.tags, ''), nw.ts, nw.temperature_c, pv.ts, pv.temperature_c
		FROM ranked nw
		LEFT JOIN ranked pv ON pv.serial = nw.serial AND pv.rn = 2
		LEFT JOIN device_notes dn ON dn.serial = nw.serial
		WHERE nw.rn = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TemperaturePair, 0, 16)
	for rows.Next() {
		var (
			serial           string
			tags             string
			nowTS            int64
			nowTemp          sql.NullInt64
			prevTS, prevTemp sql.NullInt64
		)
		if err := rows.Scan(&serial, &tags, &nowTS, &nowTemp, &prevTS, &prevTemp); err != nil {
			return nil, err
		}
		p := TemperaturePair{Serial: serial, Tags: tags}
		if nowTemp.Valid {
			v := int(nowTemp.Int64)
			p.Now = &v
		}
		// Only a reading close enough in time to be a *change* rather than a gap.
		if prevTemp.Valid && nowTS-prevTS.Int64 <= CorrelationMaxGap.Milliseconds() {
			v := int(prevTemp.Int64)
			p.Prev = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
