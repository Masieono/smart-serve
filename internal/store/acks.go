package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Ack is a device's acknowledgement of its current condition (`FEATURES.md` B7).
type Ack struct {
	Serial string
	// Health is the verdict the user acknowledged. The ack covers the drive only while its verdict
	// is still this one: a drive that gets *worse* is news, and the user acknowledged the condition
	// they could see, not whatever happens next.
	Health string
	Note   string
	At     time.Time
}

// SetAck records that the user knows about a drive's condition.
//
// `health` is the verdict being acknowledged, and the caller reads it from the stored device rather
// than taking it from the request: the ack has to name the condition the *server* last assessed, or
// a stale client could acknowledge a verdict the drive no longer has and silence a real change.
func (s *Store) SetAck(ctx context.Context, serial, health, note string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO device_notes (serial, ack_health, ack_note, ack_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			ack_health = excluded.ack_health,
			ack_note   = excluded.ack_note,
			ack_at     = excluded.ack_at,
			updated_at = excluded.updated_at`,
		serial, health, note, time.Now().UnixMilli(), time.Now().UnixMilli())
	return err
}

// ClearAck removes an acknowledgement, so the drive alerts again on the next scan.
func (s *Store) ClearAck(ctx context.Context, serial string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE device_notes SET ack_health = '', ack_note = '', ack_at = 0, updated_at = ?
		WHERE serial = ?`, time.Now().UnixMilli(), serial)
	return err
}

// AckFor returns a device's acknowledgement, or nil when it has none.
func (s *Store) AckFor(ctx context.Context, serial string) (*Ack, error) {
	var (
		a  Ack
		at int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT serial, ack_health, ack_note, ack_at FROM device_notes
		WHERE serial = ? AND ack_health <> ''`, serial).
		Scan(&a.Serial, &a.Health, &a.Note, &at)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	a.At = time.UnixMilli(at)
	return &a, nil
}
