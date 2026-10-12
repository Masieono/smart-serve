package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Channel is a configured notification destination.
type Channel struct {
	ID      int64
	Type    string // ntfy | webhook | ...
	Label   string
	Config  string // type-specific JSON (docs/NOTIFICATIONS.md)
	Enabled bool
	Events  string // JSON array of subscribed event types
}

// ListChannels returns every notification channel.
func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, label, config, enabled, events
		FROM notification_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Channel, 0, 4)
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChannel returns one channel, or nil when it does not exist.
func (s *Store) GetChannel(ctx context.Context, id int64) (*Channel, error) {
	c, err := scanChannel(s.db.QueryRowContext(ctx, `
		SELECT id, type, label, config, enabled, events
		FROM notification_channels WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateChannel inserts a channel and returns its id.
func (s *Store) CreateChannel(ctx context.Context, c Channel) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_channels (type, label, config, enabled, events)
		VALUES (?, ?, ?, ?, ?)`,
		c.Type, c.Label, c.Config, boolInt(c.Enabled), c.Events)
	if err != nil {
		return 0, fmt.Errorf("create channel: %w", err)
	}
	return res.LastInsertId()
}

// UpdateChannel replaces a channel's mutable fields.
func (s *Store) UpdateChannel(ctx context.Context, c Channel) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE notification_channels
		SET type = ?, label = ?, config = ?, enabled = ?, events = ?
		WHERE id = ?`,
		c.Type, c.Label, c.Config, boolInt(c.Enabled), c.Events, c.ID)
	if err != nil {
		return fmt.Errorf("update channel %d: %w", c.ID, err)
	}
	return nil
}

// DeleteChannel removes a channel and (via ON DELETE CASCADE) its log rows.
func (s *Store) DeleteChannel(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	return err
}

func scanChannel(r rowScanner) (Channel, error) {
	var (
		c       Channel
		enabled int64
	)
	if err := r.Scan(&c.ID, &c.Type, &c.Label, &c.Config, &enabled, &c.Events); err != nil {
		return c, err
	}
	c.Enabled = enabled != 0
	return c, nil
}

// NotificationEntry is one row of notification delivery history.
type NotificationEntry struct {
	ID        int64
	ChannelID int64
	EventType string
	EventKey  string
	SentAt    time.Time
	Status    string // sent | failed | squelched
	Detail    string
}

// LogNotification records a delivery attempt (docs/NOTIFICATIONS.md).
func (s *Store) LogNotification(ctx context.Context, channelID int64, eventType, eventKey string, at time.Time, status, detail string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_log (channel_id, event_type, event_key, sent_at, status, detail)
		VALUES (?, ?, ?, ?, ?, ?)`,
		channelID, eventType, eventKey, at.UnixMilli(), status, detail)
	if err != nil {
		return fmt.Errorf("log notification: %w", err)
	}
	return nil
}

// RecentNotifications returns the newest delivery records first.
func (s *Store) RecentNotifications(ctx context.Context, limit int) ([]NotificationEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, channel_id, event_type, event_key, sent_at, status, detail
		FROM notification_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]NotificationEntry, 0, limit)
	for rows.Next() {
		var (
			e      NotificationEntry
			sentAt int64
		)
		if err := rows.Scan(&e.ID, &e.ChannelID, &e.EventType, &e.EventKey, &sentAt,
			&e.Status, &e.Detail); err != nil {
			return nil, err
		}
		e.SentAt = fromMS(sentAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

// LastNotificationAt returns the most recent time a channel sent (or squelched)
// a given event key, which is the basis for squelch decisions.
func (s *Store) LastNotificationAt(ctx context.Context, channelID int64, eventKey string) (*time.Time, error) {
	var sentAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT sent_at FROM notification_log
		WHERE channel_id = ? AND event_key = ?
		ORDER BY sent_at DESC LIMIT 1`, channelID, eventKey).Scan(&sentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t := fromMS(sentAt)
	return &t, nil
}
