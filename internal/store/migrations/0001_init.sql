-- 0001_init.sql — Smart-Serve initial schema (docs/DATA_MODEL.md)

-- Current known state of each physical drive (upserted per scan).
CREATE TABLE devices (
    serial           TEXT PRIMARY KEY,
    model            TEXT NOT NULL DEFAULT '',
    model_family     TEXT NOT NULL DEFAULT '',
    device_path      TEXT NOT NULL DEFAULT '',
    transport        TEXT NOT NULL DEFAULT '',
    smart_bus        TEXT NOT NULL DEFAULT '',
    interface_speed  TEXT NOT NULL DEFAULT '',
    rotation         INTEGER,
    capacity_bytes   INTEGER,
    wwn              TEXT NOT NULL DEFAULT '',
    smart_available  INTEGER NOT NULL DEFAULT 0,
    smart_error      TEXT NOT NULL DEFAULT '',
    health           TEXT NOT NULL DEFAULT 'unknown',
    health_rationale TEXT NOT NULL DEFAULT '',
    score            INTEGER NOT NULL DEFAULT -1,
    temperature_c    INTEGER,
    first_seen       INTEGER NOT NULL,
    last_seen        INTEGER NOT NULL,
    last_scan        INTEGER,
    is_missing       INTEGER NOT NULL DEFAULT 0
);

-- User-entered metadata, keyed by serial so it survives reboot/replug.
CREATE TABLE device_notes (
    serial     TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT '',
    tags       TEXT NOT NULL DEFAULT '',
    hidden     INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);

-- Append-only history for trending and longevity estimates.
CREATE TABLE snapshots (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    serial         TEXT NOT NULL,
    ts             INTEGER NOT NULL,
    scan_id        INTEGER NOT NULL,
    health         TEXT NOT NULL,
    score          INTEGER NOT NULL DEFAULT -1,
    temperature_c  INTEGER,
    power_on_hours INTEGER,
    wear_pct       REAL,
    attributes     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_snapshots_serial_ts ON snapshots(serial, ts);
CREATE INDEX idx_snapshots_ts ON snapshots(ts);

-- One row per scan run.
CREATE TABLE scan_history (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at     INTEGER NOT NULL,
    ended_at       INTEGER NOT NULL,
    trigger        TEXT NOT NULL DEFAULT 'manual',
    ok             INTEGER NOT NULL DEFAULT 1,
    error          TEXT NOT NULL DEFAULT '',
    devices_total  INTEGER NOT NULL DEFAULT 0,
    devices_ok     INTEGER NOT NULL DEFAULT 0,
    devices_failed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_scan_history_started ON scan_history(started_at);

-- Scheduled scans.
CREATE TABLE scan_schedules (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    enabled       INTEGER NOT NULL DEFAULT 1,
    kind          TEXT NOT NULL,
    every_seconds INTEGER,
    cron          TEXT NOT NULL DEFAULT '',
    label         TEXT NOT NULL DEFAULT '',
    last_run      INTEGER,
    next_run      INTEGER,
    created_at    INTEGER NOT NULL
);

-- Notification channels (ntfy, webhook, ...).
CREATE TABLE notification_channels (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    type    TEXT NOT NULL,
    label   TEXT NOT NULL DEFAULT '',
    config  TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER NOT NULL DEFAULT 1,
    events  TEXT NOT NULL DEFAULT '[]'
);

-- Delivery history + squelch bookkeeping.
CREATE TABLE notification_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    event_key  TEXT NOT NULL,
    sent_at    INTEGER NOT NULL,
    status     TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_notification_log_sent ON notification_log(sent_at);
CREATE INDEX idx_notification_log_key ON notification_log(event_key, sent_at);

-- Application settings (key/value, JSON-encoded values).
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Remembered per-device -d transport for stubborn USB bridges.
CREATE TABLE device_transport_overrides (
    serial     TEXT PRIMARY KEY,
    smart_d    TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
