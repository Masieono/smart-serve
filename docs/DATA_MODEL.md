# Smart-Serve — Data Model

## Storage engine

**SQLite** (via a pure-Go or cgo driver — see ADR-003), single file at
`/data/smartserve.db`, WAL mode. SQLite is a library, not a daemon: it adds no idle
process, and the whole dataset — drive notes, schedules, notification config, and
history — is one file that can be backed up with `cp` while the app is stopped.

## Schema

### `devices` — current known state of each physical drive (upserted per scan)

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | internal |
| `serial` | TEXT UNIQUE | primary identity; `__nomodel:<model>` fallback when serial is empty |
| `model` | TEXT | e.g. `Samsung SSD 870 EVO 1TB` |
| `model_family` | TEXT NULL | |
| `device_path` | TEXT | last-seen path, e.g. `/dev/sda` — informational only, never identity |
| `transport` | TEXT | `sata`, `nvme`, `usb`, `sas`… |
| `smart_bus` | TEXT | `ata`, `nvme`, `scsi` |
| `interface_speed` | TEXT NULL | |
| `rotation` | INTEGER NULL | RPM (0 or NULL ⇒ likely SSD) |
| `capacity_bytes` | INTEGER NULL | |
| `wwn` | TEXT NULL | |
| `smart_available` | INTEGER | 0 when device present in lsblk but SMART unreadable |
| `smart_error` | TEXT NULL | capture failure reason |
| `health` | TEXT | `good` / `caution` / `bad` / `unknown` (see SMART_INTERPRETATION.md) |
| `health_rationale` | TEXT | human-readable why (e.g. "5 pending sectors") |
| `temperature_c` | INTEGER NULL | |
| `first_seen` / `last_seen` | INTEGER (unix ms) | |
| `last_scan` | INTEGER (unix ms) | last successful SMART read |
| `is_missing` | INTEGER | 1 after a scan where device absent → triggers "removed" event once |

### `device_notes` — user-entered metadata (1:1 with devices, editable in UI)

| Column | Type | Notes |
|---|---|---|
| `serial` | TEXT PK | FK → devices.serial |
| `name` | TEXT NULL | e.g. "boot", "backup pool" |
| `notes` | TEXT NULL | free-form |
| `tags` | TEXT NULL | comma-separated (Phase 2: table) |
| `hidden` | INTEGER DEFAULT 0 | user can hide devices they don't care about |
| `updated_at` | INTEGER (unix ms) | |

### `snapshots` — append-only history for trending & longevity (Phase 2 consumer)

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `serial` | TEXT | indexed |
| `ts` | INTEGER (unix ms) | indexed |
| `scan_id` | INTEGER | groups all snapshots of one scan |
| `health` | TEXT | |
| `temperature_c` | INTEGER NULL | |
| `power_on_hours` | INTEGER NULL | |
| `attributes` | TEXT | normalized JSON blob: ATA attributes or NVMe SMART/health log |
| `wear_pct` | REAL NULL | NVMe % used / SSD wear / NULL for HDD |

### `scan_schedules`

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `enabled` | INTEGER DEFAULT 1 | |
| `kind` | TEXT | `interval` or `cron` |
| `every_seconds` | INTEGER NULL | for `interval` |
| `cron` | TEXT NULL | 5-field cron for `cron` (evaluated by a Go cron library) |
| `label` | TEXT | user-friendly name |
| `last_run` / `next_run` | INTEGER (unix ms) | scheduler bookkeeping |
| `created_at` | INTEGER | |

### `notification_channels`

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `type` | TEXT | `ntfy`, `webhook` (Phase 2: `gotify`, `telegram`, `discord`, `smtp`) |
| `label` | TEXT | |
| `config` | TEXT | type-specific JSON (see NOTIFICATIONS.md) |
| `enabled` | INTEGER DEFAULT 1 | |
| `events` | TEXT | JSON list of subscribed event types; default: all |

### `notification_log` — delivery history + squelch bookkeeping

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `channel_id` | INTEGER | FK |
| `event_type` | TEXT | |
| `event_key` | TEXT | dedupe key, e.g. `health_change:<serial>:caution` |
| `sent_at` | INTEGER (unix ms) | |
| `status` | TEXT | `sent` / `failed` / `squelched` |
| `detail` | TEXT NULL | error or payload summary |

### `settings` — key/value app settings

| Column | Type | Notes |
|---|---|---|
| `key` | TEXT PK | e.g. `temperature_threshold_c`, `retention_days` |
| `value` | TEXT | JSON-encoded value |

**Default settings keys** (all UI-editable per `ADR-009`; env vars only seed the
operational ones — see `DEPLOYMENT.md`):

| Key | Default | Meaning |
|---|---|---|
| `temperature_threshold_c` | `55` | caution above this temperature |
| `reallocated_bad_threshold` | `10` | ID 5 raw ≥ this ⇒ Bad |
| `pending_bad_threshold` | `10` | ID 197 raw ≥ this ⇒ Bad |
| `uncorrectable_bad_threshold` | `10` | ID 198 raw ≥ this ⇒ Bad |
| `warn_on_any_nonzero` | `true` | any of ID 5/197/198 > 0 ⇒ Caution |
| `nvme_percentage_used_warn` | `90` | NVMe % used ≥ this ⇒ Caution |
| `retention_days` | `180` | snapshot retention window |
| `notification_log_retention_days` | `30` | notification-log retention |
| `squelch_seconds` | `3600` | re-alert window for persistent conditions |
| `command_timeout_seconds` | `15` | per-`smartctl`/`lsblk` timeout |
| `missed_run_policy` | `"skip"` | `skip` \| `run_once` (seeded by env var) |
| `scan_exclude_devices` | `[]` | device paths/serials to skip |
| `theme` | `"auto"` | `auto` \| `light` \| `dark` \| `custom` (`UI.md` §5) |
| `theme_base` | `"dark"` | base for `custom`: `light` \| `dark` |
| `theme_accent` | `"#3b82f6"` | accent colour for `custom` themes |
| `theme_surface_tint` | `"neutral"` | `neutral` \| `slate` \| `warm` \| `cool` |
| `timezone` | `""` | schedule time zone; empty = container `TZ` (C6) |
| `health_score_enabled` | `true` | show the 0–100 score/grade (D13) |
| `selftest_enabled` | `false` | allow self-test triggering — opt-in, hardware-affecting (D10) |
| `selftest_default_kind` | `"short"` | `short` \| `long` (D10) |
| `report_export_formats` | `["txt"]` | enabled export formats (D11) |

### Future tables (Phase 2 — specified now to freeze schema direction)

- `alert_acknowledgements` (`event_key`, `acked_at`, `acked_by`, `expires_at`) —
  mutes a known issue without changing the verdict.
- `maintenance_windows` (`id`, `label`, `starts_at`, `ends_at`, `device_filter`) —
  suppresses notifications during known maintenance.
- `scan_history` (`id`, `scan_id`, `started_at`, `ended_at`, `trigger`, `ok`,
  `error`, `devices_total`, `devices_ok`, `devices_failed`) — the scan-history log.
- `device_transport_overrides` (`serial`, `smart_d`, `updated_at`) — remembered
  `-d` transport for stubborn USB bridges (auto-discovered, user-overridable).

## Retention & pruning

- After every scan, snapshots older than `retention_days` (default 180) are deleted.
- `notification_log` rows older than 30 days are pruned in the same pass.
- Pruning is a single indexed DELETE per table; runs inline at end of scan (adds
  milliseconds), keeping the zero-idle-cost invariant.

## Migrations

Embedded numbered migrations (`migrations/0001_init.sql`, …) applied at startup inside
a transaction; a `schema_version` table tracks state. No runtime migration tooling —
the app is the only writer.

## Concurrency

All writes happen from the single scan executor goroutine or from API handler writes
(user notes/schedule edits), serialized via SQLite's own write lock plus a coarse
application-level mutex around transactions. WAL mode allows reads (page loads) to
never block during scans.
