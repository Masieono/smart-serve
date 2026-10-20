package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/model"
)

// ScannedDevice is one device's result from a scan, ready to persist.
type ScannedDevice struct {
	Serial         string
	Model          string
	ModelFamily    string
	Firmware       string
	DevicePath     string
	Transport      string
	SmartBus       string
	InterfaceSpeed string
	CapacityBytes  *uint64
	Rotation       *int
	// Removable is nil when lsblk did not say, which is different from "no".
	Removable       *bool
	FsVersion       string
	WWN             string
	Mountpoints     []string
	Fstype          string
	Health          string // good | caution | bad | unknown
	HealthRationale string
	Score           int
	TemperatureC    *int
	PowerOnHours    *int64
	WearPct         *float64
	// Sectors is the total of the bad-sector attributes (5 + 197 + 198) at this scan.
	// Kept per snapshot so a trend can be computed without loading the attributes blob,
	// and left nil when the drive does not report sector counts at all.
	Sectors        *int64
	AttributesJSON string
	// RawOutput is smartctl's own output for this drive, kept verbatim: when a bridge
	// misbehaves it is the only thing that explains why.
	RawOutput string
	// Error is non-empty when S.M.A.R.T. could not be read for this device.
	Error string
	// SelfTest is the newest *finished* test in the drive's self-test log, nil when the log
	// holds none. Recorded so the next scan can tell a new result from the one already
	// reported: a notification needs a transition, and a transition needs a previous value
	// (`FEATURES.md` B9).
	SelfTest *model.SelfTestEntry
}

// DeviceRecord is the persisted current state of one drive, joined with its
// user metadata.
type DeviceRecord struct {
	Serial         string
	Model          string
	ModelFamily    string
	Firmware       string
	DevicePath     string
	Transport      string
	SmartBus       string
	InterfaceSpeed string
	Rotation       *int
	CapacityBytes  *uint64
	WWN            string
	// Removable and FsVersion come from lsblk, and are often the only description
	// available for a drive that reports no S.M.A.R.T.
	Removable *bool
	FsVersion string
	// RawOutput is the last raw smartctl output for this drive. It lives here rather
	// than only in the scanner's memory, so it survives a restart and a single-drive
	// rescan of a different drive.
	RawOutput string
	// TransportOverride is the remembered `smartctl -d` for a stubborn USB bridge.
	TransportOverride string
	SmartAvailable    bool
	SmartError        string
	Health            string
	HealthRationale   string
	Score             int
	TemperatureC      *int
	Mountpoints       []string
	Fstype            string
	FirstSeen         time.Time
	LastSeen          time.Time
	LastScan          *time.Time
	IsMissing         bool

	// User metadata (device_notes).
	Name   string
	Notes  string
	Tags   string
	Hidden bool
	// Bay is where the drive physically is — "bay 4", "front top", "USB port 2". A serial
	// identifies a drive; a bay *finds* it, which is the question someone standing in front of
	// a machine at 3am actually has.
	Bay string
}

// SaveScan persists one scan atomically: a scan_history row, an upsert per
// device, a snapshot per device, and missing-marking for devices that did not
// appear in this scan. It returns the scan id.
func (s *Store) SaveScan(ctx context.Context, trigger string, started, ended time.Time, devices []ScannedDevice) (int64, error) {
	return s.saveScan(ctx, trigger, started, ended, devices, true)
}

// SavePartialScan persists a scan that only looked at some drives.
//
// It deliberately skips the "mark everything else missing" step: the drives it did
// not look at were never checked, so reporting them as disconnected would be a lie.
func (s *Store) SavePartialScan(ctx context.Context, trigger string, started, ended time.Time, devices []ScannedDevice) (int64, error) {
	return s.saveScan(ctx, trigger, started, ended, devices, false)
}

func (s *Store) saveScan(ctx context.Context, trigger string, started, ended time.Time, devices []ScannedDevice, markAbsent bool) (int64, error) {
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
			(serial, ts, scan_id, health, score, temperature_c, power_on_hours, wear_pct, sectors, attributes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.Serial, ended.UnixMilli(), scanID, d.Health, d.Score,
			nullInt(d.TemperatureC), nullInt64(d.PowerOnHours), nullFloat(d.WearPct),
			nullInt64(d.Sectors), d.AttributesJSON); err != nil {
			return 0, fmt.Errorf("insert snapshot for %s: %w", d.Serial, err)
		}
	}

	if markAbsent {
		if err := markMissing(ctx, tx, seen); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE scan_history SET devices_ok = ?, devices_failed = ? WHERE id = ?`,
		okCount, failCount, scanID); err != nil {
		return 0, err
	}
	return scanID, tx.Commit()
}

// DeleteDevice forgets a drive completely: the device row, its snapshots, and its
// user metadata.
//
// Scan history is deliberately left alone. It is a record of what happened, not of
// what exists, and deleting rows from it would rewrite the past.
func (s *Store) DeleteDevice(ctx context.Context, serial string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		`DELETE FROM snapshots WHERE serial = ?`,
		`DELETE FROM device_notes WHERE serial = ?`,
		`DELETE FROM device_transport_overrides WHERE serial = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, serial); err != nil {
			return false, fmt.Errorf("forget device %s: %w", serial, err)
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE serial = ?`, serial)
	if err != nil {
		return false, fmt.Errorf("forget device %s: %w", serial, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return affected > 0, nil
}

func upsertDevice(ctx context.Context, tx *sql.Tx, d ScannedDevice, lastScan any, started, ended time.Time) error {
	available := boolInt(d.Error == "")
	// A failed read records nothing about the self-test log: the drive was never asked, so
	// overwriting a known result with "no result" would turn the next successful scan into a
	// spurious "new result" notification. COALESCE keeps the previous value, the same way
	// last_scan does.
	//
	// A read that *succeeded* always records something, even when that something is the empty
	// string. That is what leaves SQL NULL meaning only "this row predates the migration", and
	// so what makes an empty log distinguishable from a drive we have never read
	// (0007_device_selftest.sql).
	var selfType, selfHours, selfPassed, selfStatus any
	if d.Error == "" {
		selfType, selfStatus = "", ""
		if st := d.SelfTest; st != nil {
			selfType, selfStatus = st.Type, st.Status
			selfHours, selfPassed = nullInt64(st.Hours), nullBool(st.Passed)
		}
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO devices (serial, model, model_family, firmware, device_path, transport,
			smart_bus, interface_speed, capacity_bytes, rotation, wwn, smart_available,
			smart_error, health, health_rationale, score, temperature_c, first_seen, last_seen,
			last_scan, mountpoints, fstype, removable, fs_version, raw_output, is_missing,
			selftest_type, selftest_hours, selftest_passed, selftest_status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?)
		ON CONFLICT(serial) DO UPDATE SET
			model=excluded.model,
			model_family=excluded.model_family,
			firmware=excluded.firmware,
			device_path=excluded.device_path,
			transport=excluded.transport,
			smart_bus=excluded.smart_bus,
			interface_speed=excluded.interface_speed,
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
			removable=excluded.removable,
			fs_version=excluded.fs_version,
			raw_output=excluded.raw_output,
			selftest_type=COALESCE(excluded.selftest_type, devices.selftest_type),
			selftest_hours=COALESCE(excluded.selftest_hours, devices.selftest_hours),
			selftest_passed=COALESCE(excluded.selftest_passed, devices.selftest_passed),
			selftest_status=COALESCE(excluded.selftest_status, devices.selftest_status),
			is_missing=0`,
		d.Serial, d.Model, d.ModelFamily, d.Firmware, d.DevicePath, d.Transport, d.SmartBus,
		d.InterfaceSpeed, nullUint(d.CapacityBytes), nullInt(d.Rotation), d.WWN, available,
		d.Error, d.Health, d.HealthRationale, d.Score, nullInt(d.TemperatureC),
		started.UnixMilli(), ended.UnixMilli(), lastScan,
		encodeMountpoints(d.Mountpoints), d.Fstype, nullBool(d.Removable), d.FsVersion,
		d.RawOutput, selfType, selfHours, selfPassed, selfStatus)
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
SELECT d.serial, d.model, d.model_family, d.firmware, d.device_path, d.transport, d.smart_bus,
       d.interface_speed, d.rotation, d.capacity_bytes, d.wwn, d.smart_available,
       d.smart_error, d.health, d.health_rationale, d.score, d.temperature_c,
       d.first_seen, d.last_seen, d.last_scan, d.is_missing,
       d.mountpoints, d.fstype, d.removable, d.fs_version, d.raw_output,
       COALESCE(t.smart_d, ''),
       COALESCE(n.name, ''), COALESCE(n.notes, ''), COALESCE(n.tags, ''), COALESCE(n.hidden, 0),
       COALESCE(n.bay, '')
FROM devices d
LEFT JOIN device_notes n ON n.serial = d.serial
LEFT JOIN device_transport_overrides t ON t.serial = d.serial`

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
		removable                         sql.NullInt64
		lastScan                          sql.NullInt64
		firstSeen, lastSeen               int64
		smartAvailable, isMissing, hidden int64
		mountpoints                       string
	)
	err := r.Scan(
		&rec.Serial, &rec.Model, &rec.ModelFamily, &rec.Firmware, &rec.DevicePath, &rec.Transport,
		&rec.SmartBus, &rec.InterfaceSpeed, &rotation, &capacity, &rec.WWN, &smartAvailable,
		&rec.SmartError, &rec.Health, &rec.HealthRationale, &rec.Score, &temperature,
		&firstSeen, &lastSeen, &lastScan, &isMissing,
		&mountpoints, &rec.Fstype, &removable, &rec.FsVersion, &rec.RawOutput,
		&rec.TransportOverride,
		&rec.Name, &rec.Notes, &rec.Tags, &hidden,
		&rec.Bay,
	)
	if err != nil {
		return rec, err
	}
	rec.Mountpoints = decodeMountpoints(mountpoints)
	rec.Rotation = intPtr(rotation)
	rec.CapacityBytes = uint64Ptr(capacity)
	rec.TemperatureC = intPtr(temperature)
	rec.Removable = boolPtr(removable)
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

// nullBool stores a tri-state flag, where nil (nobody said) is SQL NULL rather than
// a false that would read as a definite "no".
func nullBool(b *bool) any {
	if b == nil {
		return nil
	}
	return boolInt(*b)
}

// boolPtr reads a tri-state flag back, where SQL NULL means "not reported".
func boolPtr(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Int64 != 0
	return &b
}

// Notes is the user-entered metadata for a device.
type Notes struct {
	Serial    string
	Name      string
	Notes     string
	Tags      string
	Hidden    bool
	Bay       string
	UpdatedAt time.Time
}

// SetNotes creates or replaces the user metadata for a device. The device need
// not currently be attached; notes are keyed by serial so they survive unplugs.
func (s *Store) SetNotes(ctx context.Context, n Notes) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO device_notes (serial, name, notes, tags, hidden, bay, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			name=excluded.name, notes=excluded.notes, tags=excluded.tags,
			hidden=excluded.hidden, bay=excluded.bay, updated_at=excluded.updated_at`,
		n.Serial, n.Name, n.Notes, n.Tags, boolInt(n.Hidden), n.Bay, time.Now().UnixMilli())
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
		SELECT serial, name, notes, tags, hidden, bay, updated_at
		FROM device_notes WHERE serial = ?`, serial).
		Scan(&n.Serial, &n.Name, &n.Notes, &n.Tags, &hidden, &n.Bay, &updatedAt)
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

// AllNotes returns every device's user metadata, ordered by serial.
//
// Read from `device_notes` rather than from the joined device list, because that is where the
// labels actually live: a drive that is currently unplugged still has a name, a bay and tags, and
// those are precisely the things an export exists to preserve (`FEATURES.md` C7).
func (s *Store) AllNotes(ctx context.Context) ([]Notes, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT serial, name, notes, tags, hidden, bay, updated_at
		FROM device_notes ORDER BY serial`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Notes, 0, 8)
	for rows.Next() {
		var (
			n         Notes
			hidden    int64
			updatedAt int64
		)
		if err := rows.Scan(&n.Serial, &n.Name, &n.Notes, &n.Tags, &hidden, &n.Bay,
			&updatedAt); err != nil {
			return nil, err
		}
		n.Hidden = hidden != 0
		n.UpdatedAt = fromMS(updatedAt)
		out = append(out, n)
	}
	return out, rows.Err()
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
	// Sectors is the bad-sector total recorded at this snapshot. Nil means the drive does
	// not report one (NVMe), or the row predates migration 0006.
	Sectors *int64
	// Attributes is the normalized JSON blob. It is populated by LatestSnapshot
	// only, to avoid loading heavy blobs for whole series.
	Attributes string
}

// SectorTrend returns, per serial, how many bad sectors appeared over the *recent*
// comparison window — not over the whole range.
//
// The recent window is what makes the dashboard's warning decay. A drive that gained
// sectors five months ago and has been stable since is not getting worse, and a badge that
// says otherwise for six months is a badge people learn to ignore. The rule matches
// interp.worsening exactly, including its fallback: when the whole history is younger than
// `recent`, the comparison is against the oldest reading, so a fresh install that is
// already shedding sectors still gets flagged.
//
// One query rather than one per device, because the dashboard needs this for every drive
// at once. Rows are folded in Go rather than using a correlated subquery per device, and
// the fold keeps only two rows per device: the newest, and the newest that is old enough
// to compare against. Rows arrive oldest-first, so nothing that qualifies can arrive later.
// SnapshotFacts is the newest snapshot's per-drive figures.
//
// Power-on hours and wear live on snapshots rather than on `devices`, because they are readings
// rather than current state. A fleet table needs them for every row at once, so they are fetched
// in one query for the whole list instead of one query per drive.
type SnapshotFacts struct {
	PowerOnHours *int64
	WearPct      *float64
}

// LatestSnapshotFacts returns the newest snapshot's figures for every drive that has one.
//
// Keyed on the highest snapshot id rather than the highest timestamp: two snapshots can share a
// millisecond when a partial rescan follows a full one, and "the latest" has to be one row.
func (s *Store) LatestSnapshotFacts(ctx context.Context) (map[string]SnapshotFacts, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.serial, s.power_on_hours, s.wear_pct
		FROM snapshots s
		JOIN (SELECT serial, MAX(id) AS id FROM snapshots GROUP BY serial) newest
		  ON newest.id = s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]SnapshotFacts{}
	for rows.Next() {
		var (
			serial string
			hours  sql.NullInt64
			wear   sql.NullFloat64
		)
		if err := rows.Scan(&serial, &hours, &wear); err != nil {
			return nil, err
		}
		f := SnapshotFacts{}
		if hours.Valid {
			v := hours.Int64
			f.PowerOnHours = &v
		}
		if wear.Valid {
			v := wear.Float64
			f.WearPct = &v
		}
		out[serial] = f
	}
	return out, rows.Err()
}

// SectorTrend reports how many bad sectors appeared per device over the recent window.
func (s *Store) SectorTrend(ctx context.Context, from time.Time, recent time.Duration) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT serial, ts, sectors FROM snapshots
		WHERE sectors IS NOT NULL AND ts >= ?
		ORDER BY serial, ts`, from.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type point struct {
		ts      int64
		sectors int64
	}
	type state struct {
		first   point
		base    point
		last    point
		hasBase bool
		seen    int
	}

	recentMS := recent.Milliseconds()
	states := map[string]*state{}
	for rows.Next() {
		var (
			serial string
			pt     point
		)
		if err := rows.Scan(&serial, &pt.ts, &pt.sectors); err != nil {
			return nil, err
		}
		st, ok := states[serial]
		if !ok {
			st = &state{first: pt}
			states[serial] = st
		} else if pt.ts-st.last.ts >= recentMS {
			// The row we saw before becomes the base: it is the newest row at least
			// `recent` older than the row arriving now, which is the newest we have.
			st.base, st.hasBase = st.last, true
		}
		st.last = pt
		st.seen++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make(map[string]int64, len(states))
	for serial, st := range states {
		if st.seen < 2 {
			// One reading is not a trend, and saying "flat" would be inventing history.
			continue
		}
		base := st.first
		if st.hasBase {
			base = st.base
		}
		out[serial] = st.last.sectors - base.sectors
	}
	return out, nil
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
		sectors     sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, serial, ts, scan_id, health, score, temperature_c, power_on_hours, wear_pct, sectors, attributes
		FROM snapshots WHERE serial = ? ORDER BY ts DESC LIMIT 1`, serial).
		Scan(&snap.ID, &snap.Serial, &ts, &snap.ScanID, &snap.Health, &snap.Score,
			&temperature, &hours, &wear, &sectors, &snap.Attributes)
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
	if sectors.Valid {
		v := sectors.Int64
		snap.Sectors = &v
	}
	return &snap, nil
}

// RecentSnapshots returns the newest `limit` snapshots for a device, newest first, including each
// one's attributes blob.
//
// `SnapshotSeries` deliberately omits that blob because the trend chart asks for hundreds of rows;
// a diff needs exactly two and needs what is inside them, so it gets its own query rather than a
// flag on that one.
func (s *Store) RecentSnapshots(ctx context.Context, serial string, limit int) ([]Snapshot, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, serial, ts, scan_id, health, score, temperature_c, power_on_hours, wear_pct,
		       sectors, COALESCE(attributes, '')
		FROM snapshots WHERE serial = ? ORDER BY id DESC LIMIT ?`, serial, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Snapshot, 0, limit)
	for rows.Next() {
		var (
			snap        Snapshot
			ts          int64
			temperature sql.NullInt64
			hours       sql.NullInt64
			wear        sql.NullFloat64
			sectors     sql.NullInt64
		)
		if err := rows.Scan(&snap.ID, &snap.Serial, &ts, &snap.ScanID, &snap.Health, &snap.Score,
			&temperature, &hours, &wear, &sectors, &snap.Attributes); err != nil {
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
		if sectors.Valid {
			v := sectors.Int64
			snap.Sectors = &v
		}
		out = append(out, snap)
	}
	return out, rows.Err()
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
		SELECT id, serial, ts, scan_id, health, score, temperature_c, power_on_hours, wear_pct, sectors
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
			sectors      sql.NullInt64
		)
		if err := rows.Scan(&snap.ID, &snap.Serial, &ts, &snap.ScanID, &snap.Health,
			&snap.Score, &temperature, &powerOnHours, &wearPct, &sectors); err != nil {
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
		if sectors.Valid {
			v := sectors.Int64
			snap.Sectors = &v
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

// DeviceState is the slice of a device's state the notifier compares between
// scans (docs/NOTIFICATIONS.md).
type DeviceState struct {
	Serial          string
	Name            string // user-assigned name, when one is set
	Model           string
	Health          string
	HealthRationale string
	TemperatureC    *int
	// Tags is the drive's stored tag string, carried because the post-scan pass groups drives by it
	// (`FEATURES.md` A25) and this is the one query that already joins the notes.
	Tags string
	// Missing is true when the device was absent from the most recent scan.
	Missing bool
	// SelfTestRecorded is false until a scan has stored what this drive's self-test log holds.
	// It is what tells "the log is empty" apart from "we have never looked", and only the first
	// is a baseline a new result can differ from — without it, every pre-existing result would
	// look like news the first time a drive was scanned after an upgrade (`FEATURES.md` B9).
	SelfTestRecorded bool
	// SelfTest is the newest finished test in the drive's log, nil when there is none.
	SelfTest *model.SelfTestEntry
}

// DeviceStates returns every known device keyed by serial, joined with its
// user-assigned name.
//
// Call it twice around a scan to get a transition: before the scan for the
// previous state, after for the new one. Because SaveScan flags absent devices
// rather than deleting them, a state with Missing set is one that has just
// disappeared, and one that is present after being Missing has reappeared.
func (s *Store) DeviceStates(ctx context.Context) (map[string]DeviceState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.serial, COALESCE(n.name, ''), d.model, d.health, d.health_rationale,
		       d.temperature_c, d.is_missing,
		       COALESCE(n.tags, ''),
		       d.selftest_type IS NOT NULL,
		       COALESCE(d.selftest_type, ''), d.selftest_hours, d.selftest_passed,
		       COALESCE(d.selftest_status, '')
		FROM devices d
		LEFT JOIN device_notes n ON n.serial = d.serial`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]DeviceState{}
	for rows.Next() {
		var (
			st        DeviceState
			temp      sql.NullInt64
			isMissing int64
			recorded  bool
			selfType  string
			selfHours sql.NullInt64
			selfPass  sql.NullInt64
			selfStat  string
		)
		if err := rows.Scan(&st.Serial, &st.Name, &st.Model, &st.Health,
			&st.HealthRationale, &temp, &isMissing, &st.Tags,
			&recorded, &selfType, &selfHours, &selfPass, &selfStat); err != nil {
			return nil, err
		}
		st.TemperatureC = intPtr(temp)
		st.Missing = isMissing != 0
		st.SelfTestRecorded = recorded
		// A recorded empty type is a log that was read and holds no finished test, so there is
		// no entry to carry — as opposed to a row this migration has never seen.
		if recorded && selfType != "" {
			st.SelfTest = &model.SelfTestEntry{
				Type:   selfType,
				Hours:  int64Ptr(selfHours),
				Passed: boolPtr(selfPass),
				Status: selfStat,
			}
		}
		out[st.Serial] = st
	}
	return out, rows.Err()
}
