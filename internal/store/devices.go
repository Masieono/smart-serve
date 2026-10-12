package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ScannedDevice is one device's result from a scan, ready to persist.
type ScannedDevice struct {
	Serial          string
	Model           string
	DevicePath      string
	Transport       string
	SmartBus        string
	CapacityBytes   *uint64
	Rotation        *int
	WWN             string
	Mountpoints     []string
	Fstype          string
	Health          string // good | caution | bad | unknown
	HealthRationale string
	Score           int
	TemperatureC    *int
	PowerOnHours    *int64
	WearPct         *float64
	AttributesJSON  string
	// Error is non-empty when S.M.A.R.T. could not be read for this device.
	Error string
}

// DeviceRecord is the persisted current state of one drive, joined with its
// user metadata.
type DeviceRecord struct {
	Serial          string
	Model           string
	ModelFamily     string
	DevicePath      string
	Transport       string
	SmartBus        string
	InterfaceSpeed  string
	Rotation        *int
	CapacityBytes   *uint64
	WWN             string
	SmartAvailable  bool
	SmartError      string
	Health          string
	HealthRationale string
	Score           int
	TemperatureC    *int
	Mountpoints     []string
	Fstype          string
	FirstSeen       time.Time
	LastSeen        time.Time
	LastScan        *time.Time
	IsMissing       bool

	// User metadata (device_notes).
	Name   string
	Notes  string
	Tags   string
	Hidden bool
}

// SaveScan persists one scan atomically: a scan_history row, an upsert per
// device, a snapshot per device, and missing-marking for devices that did not
// appear in this scan. It returns the scan id.
func (s *Store) SaveScan(ctx context.Context, trigger string, started, ended time.Time, devices []ScannedDevice) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `INSERT INTO scan_history
		(started_at, ended_at, trigger, ok, error, devices_total, devices_ok, devices_failed)
		VALUES (?, ?, ?, 1, '', ?, 0, 0)`,
		started.UnixMilli(), ended.UnixMilli(), trigger, len(devices))
	if err != nil {
		return 0, fmt.Errorf("insert scan history: %w", err)
	}
	scanID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	var okCount, failCount int
	seen := make([]string, 0, len(devices))
	for _, d := range devices {
		seen = append(seen, d.Serial)

		var lastScan any
		if d.Error == "" {
			lastScan = ended.UnixMilli()
			okCount++
		} else {
			failCount++
		}

		if err := upsertDevice(ctx, tx, d, lastScan, started, ended); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO snapshots
			(serial, ts, scan_id, health, score, temperature_c, power_on_hours, wear_pct, attributes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.Serial, ended.UnixMilli(), scanID, d.Health, d.Score,
			nullInt(d.TemperatureC), nullInt64(d.PowerOnHours), nullFloat(d.WearPct),
			d.AttributesJSON); err != nil {
			return 0, fmt.Errorf("insert snapshot for %s: %w", d.Serial, err)
		}
	}

	if err := markMissing(ctx, tx, seen); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE scan_history SET devices_ok = ?, devices_failed = ? WHERE id = ?`,
		okCount, failCount, scanID); err != nil {
		return 0, err
	}
	return scanID, tx.Commit()
}

func upsertDevice(ctx context.Context, tx *sql.Tx, d ScannedDevice, lastScan any, started, ended time.Time) error {
	available := boolInt(d.Error == "")
	_, err := tx.ExecContext(ctx, `
		INSERT INTO devices (serial, model, device_path, transport, smart_bus, capacity_bytes,
			rotation, wwn, smart_available, smart_error, health, health_rationale, score,
			temperature_c, first_seen, last_seen, last_scan, mountpoints, fstype, is_missing)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)
		ON CONFLICT(serial) DO UPDATE SET
			model=excluded.model,
			device_path=excluded.device_path,
			transport=excluded.transport,
			smart_bus=excluded.smart_bus,
			capacity_bytes=excluded.capacity_bytes,
			rotation=excluded.rotation,
			wwn=excluded.wwn,
			smart_available=excluded.smart_available,
			smart_error=excluded.smart_error,
			health=excluded.health,
			health_rationale=excluded.health_rationale,
			score=excluded.score,
			temperature_c=excluded.temperature_c,
			last_seen=excluded.last_seen,
			last_scan=COALESCE(excluded.last_scan, devices.last_scan),
			mountpoints=excluded.mountpoints,
			fstype=excluded.fstype,
			is_missing=0`,
		d.Serial, d.Model, d.DevicePath, d.Transport, d.SmartBus, nullUint(d.CapacityBytes),
		nullInt(d.Rotation), d.WWN, available, d.Error, d.Health, d.HealthRationale, d.Score,
		nullInt(d.TemperatureC), started.UnixMilli(), ended.UnixMilli(), lastScan,
		encodeMountpoints(d.Mountpoints), d.Fstype)
	if err != nil {
		return fmt.Errorf("upsert device %s: %w", d.Serial, err)
	}
	return nil
}

// markMissing flags every previously-seen device that did not appear in this
// scan. A device is never deleted, so its notes survive an unplug.
func markMissing(ctx context.Context, tx *sql.Tx, seen []string) error {
	if len(seen) == 0 {
		_, err := tx.ExecContext(ctx, `UPDATE devices SET is_missing = 1 WHERE is_missing = 0`)
		return err
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(seen)), ",")
	args := make([]any, 0, len(seen))
	for _, s := range seen {
		args = append(args, s)
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE devices SET is_missing = 1 WHERE is_missing = 0 AND serial NOT IN (`+placeholders+`)`,
		args...)
	return err
}

const deviceSelect = `
SELECT d.serial, d.model, d.model_family, d.device_path, d.transport, d.smart_bus,
       d.interface_speed, d.rotation, d.capacity_bytes, d.wwn, d.smart_available,
       d.smart_error, d.health, d.health_rationale, d.score, d.temperature_c,
       d.first_seen, d.last_seen, d.last_scan, d.is_missing,
       d.mountpoints, d.fstype,
       COALESCE(n.name, ''), COALESCE(n.notes, ''), COALESCE(n.tags, ''), COALESCE(n.hidden, 0)
FROM devices d
LEFT JOIN device_notes n ON n.serial = d.serial`

// ListDevices returns every known device, worst health first.
func (s *Store) ListDevices(ctx context.Context) ([]DeviceRecord, error) {
	rows, err := s.db.QueryContext(ctx, deviceSelect+`
		ORDER BY CASE d.health
			WHEN 'bad' THEN 0 WHEN 'caution' THEN 1 WHEN 'unknown' THEN 2 ELSE 3 END,
			COALESCE(NULLIF(n.name, ''), d.model)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DeviceRecord, 0, 8)
	for rows.Next() {
		rec, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetDevice returns one device, or nil when it is unknown.
func (s *Store) GetDevice(ctx context.Context, serial string) (*DeviceRecord, error) {
	rec, err := scanDevice(s.db.QueryRowContext(ctx, deviceSelect+` WHERE d.serial = ?`, serial))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func scanDevice(r rowScanner) (DeviceRecord, error) {
	var (
		rec                               DeviceRecord
		rotation, capacity, temperature   sql.NullInt64
		lastScan                          sql.NullInt64
		firstSeen, lastSeen               int64
		smartAvailable, isMissing, hidden int64
		mountpoints                       string
	)
	err := r.Scan(
		&rec.Serial, &rec.Model, &rec.ModelFamily, &rec.DevicePath, &rec.Transport,
		&rec.SmartBus, &rec.InterfaceSpeed, &rotation, &capacity, &rec.WWN, &smartAvailable,
		&rec.SmartError, &rec.Health, &rec.HealthRationale, &rec.Score, &temperature,
		&firstSeen, &lastSeen, &lastScan, &isMissing,
		&mountpoints, &rec.Fstype,
		&rec.Name, &rec.Notes, &rec.Tags, &hidden,
	)
	if err != nil {
		return rec, err
	}
	rec.Mountpoints = decodeMountpoints(mountpoints)
	rec.Rotation = intPtr(rotation)
	rec.CapacityBytes = uint64Ptr(capacity)
	rec.TemperatureC = intPtr(temperature)
	rec.SmartAvailable = smartAvailable != 0
	rec.IsMissing = isMissing != 0
	rec.Hidden = hidden != 0
	rec.FirstSeen = fromMS(firstSeen)
	rec.LastSeen = fromMS(lastSeen)
	rec.LastScan = timePtr(lastScan)
	return rec, nil
}

// encodeMountpoints stores the mount list as a JSON array.
func encodeMountpoints(mp []string) string {
	if len(mp) == 0 {
		return "[]"
	}
	b, err := json.Marshal(mp)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeMountpoints(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// Notes is the user-entered metadata for a device.
type Notes struct {
	Serial    string
	Name      string
	Notes     string
	Tags      string
	Hidden    bool
	UpdatedAt time.Time
}

// SetNotes creates or replaces the user metadata for a device. The device need
// not currently be attached; notes are keyed by serial so they survive unplugs.
func (s *Store) SetNotes(ctx context.Context, n Notes) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO device_notes (serial, name, notes, tags, hidden, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			name=excluded.name, notes=excluded.notes, tags=excluded.tags,
			hidden=excluded.hidden, updated_at=excluded.updated_at`,
		n.Serial, n.Name, n.Notes, n.Tags, boolInt(n.Hidden), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("set notes for %s: %w", n.Serial, err)
	}
	return nil
}

// GetNotes returns the user metadata for a device, or nil when there is none.
func (s *Store) GetNotes(ctx context.Context, serial string) (*Notes, error) {
	var (
		n         Notes
		hidden    int64
		updatedAt int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT serial, name, notes, tags, hidden, updated_at
		FROM device_notes WHERE serial = ?`, serial).
		Scan(&n.Serial, &n.Name, &n.Notes, &n.Tags, &hidden, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.Hidden = hidden != 0
	n.UpdatedAt = fromMS(updatedAt)
	return &n, nil
}

// SetTransportOverride remembers the working -d transport for a stubborn USB
// bridge (docs/FEATURES.md A11).
func (s *Store) SetTransportOverride(ctx context.Context, serial, smartD string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO device_transport_overrides (serial, smart_d, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			smart_d=excluded.smart_d, updated_at=excluded.updated_at`,
		serial, smartD, time.Now().UnixMilli())
	return err
}

// GetTransportOverride returns the remembered -d transport, or "" when none.
func (s *Store) GetTransportOverride(ctx context.Context, serial string) (string, error) {
	var d string
	err := s.db.QueryRowContext(ctx,
		`SELECT smart_d FROM device_transport_overrides WHERE serial = ?`, serial).Scan(&d)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return d, err
}

// ScanHistoryEntry is one recorded scan run.
type ScanHistoryEntry struct {
	ID            int64
	StartedAt     time.Time
	EndedAt       time.Time
	Trigger       string
	OK            bool
	Error         string
	DevicesTotal  int
	DevicesOK     int
	DevicesFailed int
}

// ListScanHistory returns the most recent scans, newest first.
func (s *Store) ListScanHistory(ctx context.Context, limit int) ([]ScanHistoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, started_at, ended_at, trigger, ok, error,
		       devices_total, devices_ok, devices_failed
		FROM scan_history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ScanHistoryEntry, 0, limit)
	for rows.Next() {
		var (
			e                  ScanHistoryEntry
			startedAt, endedAt int64
			ok                 int64
		)
		if err := rows.Scan(&e.ID, &startedAt, &endedAt, &e.Trigger, &ok, &e.Error,
			&e.DevicesTotal, &e.DevicesOK, &e.DevicesFailed); err != nil {
			return nil, err
		}
		e.StartedAt = fromMS(startedAt)
		e.EndedAt = fromMS(endedAt)
		e.OK = ok != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

// Snapshot is one recorded point in a device's history.
type Snapshot struct {
	ID           int64
	Serial       string
	TS           time.Time
	ScanID       int64
	Health       string
	Score        int
	TemperatureC *int
	PowerOnHours *int64
	WearPct      *float64
	// Attributes is the normalized JSON blob. It is populated by LatestSnapshot
	// only, to avoid loading heavy blobs for whole series.
	Attributes string
}

// LatestSnapshot returns the most recent snapshot for a device, including the
// normalized attributes blob, or nil when the device has no history.
func (s *Store) LatestSnapshot(ctx context.Context, serial string) (*Snapshot, error) {
	var (
		snap        Snapshot
		ts          int64
		temperature sql.NullInt64
		hours       sql.NullInt64
		wear        sql.NullFloat64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, serial, ts, scan_id, health, score, temperature_c, power_on_hours, wear_pct, attributes
		FROM snapshots WHERE serial = ? ORDER BY ts DESC LIMIT 1`, serial).
		Scan(&snap.ID, &snap.Serial, &ts, &snap.ScanID, &snap.Health, &snap.Score,
			&temperature, &hours, &wear, &snap.Attributes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snap.TS = fromMS(ts)
	snap.TemperatureC = intPtr(temperature)
	if hours.Valid {
		v := hours.Int64
		snap.PowerOnHours = &v
	}
	if wear.Valid {
		v := wear.Float64
		snap.WearPct = &v
	}
	return &snap, nil
}

// SaveFailedScan records a scan that failed before any device could be read.
func (s *Store) SaveFailedScan(ctx context.Context, trigger string, started, ended time.Time, errMsg string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO scan_history
		(started_at, ended_at, trigger, ok, error, devices_total, devices_ok, devices_failed)
		VALUES (?, ?, ?, 0, ?, 0, 0, 0)`,
		started.UnixMilli(), ended.UnixMilli(), trigger, errMsg)
	if err != nil {
		return 0, fmt.Errorf("insert failed scan: %w", err)
	}
	return res.LastInsertId()
}

// SnapshotSeries returns the recorded history for one device within a range.
func (s *Store) SnapshotSeries(ctx context.Context, serial string, from, to time.Time) ([]Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, serial, ts, scan_id, health, score, temperature_c, power_on_hours, wear_pct
		FROM snapshots WHERE serial = ? AND ts >= ? AND ts <= ?
		ORDER BY ts`, serial, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Snapshot, 0, 32)
	for rows.Next() {
		var (
			snap         Snapshot
			ts           int64
			temperature  sql.NullInt64
			powerOnHours sql.NullInt64
			wearPct      sql.NullFloat64
		)
		if err := rows.Scan(&snap.ID, &snap.Serial, &ts, &snap.ScanID, &snap.Health,
			&snap.Score, &temperature, &powerOnHours, &wearPct); err != nil {
			return nil, err
		}
		snap.TS = fromMS(ts)
		snap.TemperatureC = intPtr(temperature)
		if powerOnHours.Valid {
			v := powerOnHours.Int64
			snap.PowerOnHours = &v
		}
		if wearPct.Valid {
			v := wearPct.Float64
			snap.WearPct = &v
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

// Prune deletes snapshots and notification-log rows older than the retention
// windows. It is called at the end of each scan (docs/DATA_MODEL.md).
func (s *Store) Prune(ctx context.Context, retentionDays, logRetentionDays int) (int64, int64, error) {
	var snapshotsDeleted, logsDeleted int64
	if retentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -retentionDays).UnixMilli()
		res, err := s.db.ExecContext(ctx, `DELETE FROM snapshots WHERE ts < ?`, cutoff)
		if err != nil {
			return 0, 0, fmt.Errorf("prune snapshots: %w", err)
		}
		snapshotsDeleted, _ = res.RowsAffected()
	}
	if logRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -logRetentionDays).UnixMilli()
		res, err := s.db.ExecContext(ctx, `DELETE FROM notification_log WHERE sent_at < ?`, cutoff)
		if err != nil {
			return 0, 0, fmt.Errorf("prune notification log: %w", err)
		}
		logsDeleted, _ = res.RowsAffected()
	}
	return snapshotsDeleted, logsDeleted, nil
}
