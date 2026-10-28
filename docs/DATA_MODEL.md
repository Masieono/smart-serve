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
| `firmware` | TEXT NULL | firmware revision (migration 0003); useful for spotting known-bad firmware. Falls back to lsblk's `REV` when smartctl cannot be read — the only value a drive with no S.M.A.R.T. has |
| `device_path` | TEXT | last-seen path, e.g. `/dev/sda` — informational only, never identity |
| `transport` | TEXT | `sata`, `nvme`, `usb`, `sas`… |
| `smart_bus` | TEXT | `ata`, `nvme`, `scsi` |
| `interface_speed` | TEXT NULL | negotiated link speed, e.g. `6.0 Gb/s` |
| `rotation` | INTEGER NULL | RPM (0 or NULL ⇒ likely SSD) |
| `capacity_bytes` | INTEGER NULL | |
| `wwn` | TEXT NULL | |
| `mountpoints` | TEXT | JSON array, aggregated from the disk **and its partitions** — a whole-disk node is never mounted itself, so partitions are the only source of this |
| `fstype` | TEXT NULL | comma-separated filesystem types, aggregated the same way |
| `fs_version` | TEXT NULL | filesystem versions (`1.0`, `FAT32`…), aggregated the same way (migration 0004) |
| `removable` | INTEGER NULL | lsblk's `rm` (migration 0004). **Nullable on purpose**: `0` is "fixed media" and `NULL` is "nobody said", and a flash drive is a different thing from a fixed disk |
| `smart_available` | INTEGER | 0 when device present in lsblk but SMART unreadable |
| `raw_output` | TEXT | verbatim `smartctl --json` output from the last scan (migration 0005). On `devices` (latest only) rather than per-snapshot, so it cannot grow without bound. It lives here and not in the scanner's memory because the raw view has to survive a restart and a single-drive rescan of a *different* drive |
| `smart_error` | TEXT NULL | capture failure reason |
| `health` | TEXT | `good` / `caution` / `bad` / `unknown` (see SMART_INTERPRETATION.md) |
| `health_rationale` | TEXT | human-readable why (e.g. "5 pending sectors") |
| `temperature_c` | INTEGER NULL | |
| `first_seen` / `last_seen` | INTEGER (unix ms) | |
| `last_scan` | INTEGER (unix ms) | last successful SMART read |
| `is_missing` | INTEGER | 1 after a scan where device absent → triggers "removed" event once. The dashboard hides these by default (they are kept so notes and history survive) |
| `selftest_type` / `selftest_hours` / `selftest_passed` / `selftest_status` | TEXT / INTEGER / INTEGER / TEXT, all NULL | the newest **finished** self-test in the drive's log (migration 0007), kept so `B9` can tell a new result from one already reported. **NULL means "never recorded"**; `''` means "the log was read and holds nothing". Only the second is a baseline a new result can differ from, which is what stops an upgrade announcing every drive's existing results. A read that failed leaves all four untouched (`NOTIFICATIONS.md`) |

### `device_notes` — user-entered metadata (1:1 with devices, editable in UI)

| Column | Type | Notes |
|---|---|---|
| `serial` | TEXT PK | logical reference to devices.serial (no FK, so notes can exist for a device that is currently unplugged) |
| `name` | TEXT NULL | e.g. "boot", "backup pool" |
| `notes` | TEXT NULL | free-form |
| `tags` | TEXT NULL | comma-separated (Phase 2: table) |
| `hidden` | INTEGER DEFAULT 0 | user can hide devices they don't care about |
| `bay` | TEXT NOT NULL DEFAULT '' | where the drive physically is — "bay 4", "front top", "USB port 2" (migration 0008). Free text, because a bay is whatever the enclosure calls it. **Not nullable**, unlike the self-test columns: nothing about a bay is inferred from the drive, so "no bay recorded" and "the field is empty" are the same finding. It lives with the notes rather than on `devices` because it is something a person assigned, so it survives an unplug for the same reason a name does (`FEATURES.md` D10) |
| `ack_health` | TEXT NOT NULL DEFAULT '' | the verdict the user acknowledged (migration 0009, `FEATURES.md` B7). **Empty means "not acknowledged"**, so there is no third state to represent. It is what makes the ack expire: the ack covers the drive only while its verdict is still this one, so it can never hide a drive getting worse |
| `ack_note` | TEXT NOT NULL DEFAULT '' | why the alert is being ignored — "RMA raised", "known bad cable". Asked for when acknowledging, because "acknowledged" with no reason is a state nobody can act on later |
| `ack_at` | INTEGER NOT NULL DEFAULT 0 | unix ms, so the UI can say when the condition was acknowledged |
| `updated_at` | INTEGER (unix ms) | |

### `snapshots` — append-only history for trending & longevity (Phase 2 consumer)

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `serial` | TEXT | indexed |
| `ts` | INTEGER (unix ms) | indexed |
| `scan_id` | INTEGER | references `scan_history.id` |
| `health` | TEXT | |
| `score` | INTEGER | 0–100 health score, or -1 |
| `temperature_c` | INTEGER NULL | |
| `power_on_hours` | INTEGER NULL | |
| `attributes` | TEXT | normalized JSON blob: ATA attributes or NVMe SMART/health log |
| `wear_pct` | REAL NULL | Percentage of rated life **used**: NVMe `percentage_used`, or `100 − life left` for an ATA SSD (`SMART_INTERPRETATION.md` §1.1). NULL for a drive that reports neither — every HDD, and any SSD whose wear attribute is not one we trust |
| `sectors` | INTEGER NULL | reallocated + pending + uncorrectable (attrs 5/197/198), added by migration `0006`. Denormalized out of `attributes` because the trend and longevity code asks for hundreds of rows and must not load that blob; NULL means the drive does not report sector counts (every NVMe) **or** the row predates `0006`. A projection needs two points, and inventing a zero for history we never recorded would fabricate a flat trend. |

### `scan_schedules`

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `enabled` | INTEGER DEFAULT 1 | |
| `kind` | TEXT | *when* it runs: `interval` or `cron` |
| `action` | TEXT NOT NULL DEFAULT `'scan'` | *what* happens when it fires (migration 0010, `FEATURES.md` B13): `scan` or `digest`. A separate axis from `kind`, so a digest inherits cron, time zones and the missed-run policy instead of duplicating them. An empty or absent value means `scan`, which is what every row written before migration 0010 meant |
| `every_seconds` | INTEGER NULL | for `interval` |
| `cron` | TEXT NULL | 5-field cron for `cron` (evaluated by a Go cron library) |
| `label` | TEXT | user-friendly name |
| `last_run` / `next_run` | INTEGER (unix ms) | scheduler bookkeeping |
| `created_at` | INTEGER | |

### `notification_channels`

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `type` | TEXT | `ntfy`, `gotify`, `telegram`, `discord`, `smtp`, `webhook` (`NOTIFICATIONS.md`) |
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

**Default settings keys.** Every key below is editable in the UI (`ADR-009`);
environment variables only *seed* the operational ones (`DEPLOYMENT.md`). "Read by"
records where a value actually takes effect, because a setting that nothing reads is
worse than no setting at all.

| Key | Default | Meaning | Read by |
|---|---|---|---|
| `temperature_threshold_c` | `55` | caution above this temperature | `interp` (verdict), `notify` (alert) |
| `reallocated_bad_threshold` | `10` | ID 5 raw ≥ this ⇒ Bad | `interp` |
| `pending_bad_threshold` | `10` | ID 197 raw ≥ this ⇒ Bad | `interp` |
| `uncorrectable_bad_threshold` | `10` | ID 198 raw ≥ this ⇒ Bad | `interp` |
| `warn_on_any_nonzero` | `true` | any of ID 5/197/198 > 0 ⇒ Caution | `interp` |
| `nvme_percentage_used_warn` | `90` | NVMe % used ≥ this ⇒ Caution | `interp` |
| `retention_days` | `180` | snapshot retention window. **Also bounds the projection, the worsening flag and the trend adjustment** — there is one window, not two, so raising it gives a longer view as well as a longer memory (`FEATURES.md` A26) | `scan` (pruning) |
| `notification_log_retention_days` | `30` | notification-log retention | `scan` (pruning) |
| `squelch_seconds` | `3600` | re-alert window for persistent conditions | `notify` |
| `correlation_min_drives` | `4` | how many drives in one tag group must heat up together before `A25` reports it as one finding about the group. Raise it for a big enclosure: 4 is a sixth of a 24-bay NAS and nearly all of a workstation | `interp` |
| `correlation_delta_c` | `8` | the temperature **rise** between two readings that counts as a drive having moved. A stopped fan moves a whole chassis well past this; a warm afternoon usually does not | `interp` |
| `public_url` | `""` | where this app is reachable from outside the host, used for the "Open drive" action button on a notification (`FEATURES.md` B10). Empty means no button — the app cannot know its own external address, and a wrong link is worse than none | `notify` |
| `prometheus_enabled` | `false` | serve `GET /metrics` (`FEATURES.md` C6). Off by default: the endpoint costs nothing itself, but enabling it invites another service to poll this app, and that is the operator's decision | `httpapi` |
| `command_timeout_seconds` | `15` | per-`smartctl`/`lsblk` timeout | `scan` (env-seeded) |
| `missed_run_policy` | `"skip"` | `skip` \| `run_once` | `sched` (env-seeded) |
| `timezone` | `""` | schedule time zone; empty = container `TZ` (C6) | `sched` |
| `temperature_unit` | `"c"` | `c` \| `f` — display only; stored values stay Celsius | `webui`, `notify` |
| `theme` | `"auto"` | `auto` \| `light` \| `dark` \| `catppuccin-latte` \| `catppuccin-mocha` \| `dracula` \| `nord` \| `gruvbox-light` \| `gruvbox-dark` \| `custom` (`UI.md` §5) | `webui` |
| `theme_base` | `"dark"` | base for `custom`: `light` \| `dark` | `webui` |
| `theme_accent` | `"#3b82f6"` | accent colour, read **only** when `theme` is `custom` | `webui` |
| `theme_surface_tint` | `"neutral"` | `neutral` \| `slate` \| `warm` \| `cool`; remixes any theme's palette | `webui` |
| `selftest_poll_seconds` | `15` | how often the self-test card re-reads a drive while a test runs, 1–60 (`FEATURES.md` A19). The cost is device reads, and a long test moves a few percent an hour — so a fast refresh mostly spends them | `webui` |
| `dashboard_view` | `"cards"` | `cards` \| `table` — the shape of the fleet list (`FEATURES.md` D8). A display preference like the theme and the temperature unit rather than view state like the filter and the sort, so it survives a reload and travels in the config export | `webui` |
| `maintenance_windows` | `[]` | JSON list of windows — see `NOTIFICATIONS.md` for the shape. One-off (`starts_at`/`ends_at`) or recurring (`daily`/`weekly` with a time of day), optionally scoped to serials | `notify` |

Thresholds are read **per scan** (`interp.FromSettings`) rather than captured at
start-up, so a value changed in the UI applies to the very next scan. The same
applies to the squelch window, the maintenance windows, the command timeout, the
schedule timezone and the missed-run policy. A threshold whose only reader is a
`DefaultThresholds()` call at boot is a control that lies, which is exactly what this
table exists to prevent.

### Settings keys that are not implemented

Listed so the schema direction is frozen. A key that nothing reads lives here rather
than in the table above, so "what does this setting do" never has two answers.

| Key | Default | Meaning | Status |
|---|---|---|---|
| `health_score_enabled` | `true` | show the 0–100 score/grade (D13) | not implemented — the score is always computed and displayed |
| `selftest_enabled` | `false` | allow self-test triggering — opt-in, hardware-affecting (D10) | not implemented — the opt-in is the button itself, so there is nothing to switch |
| `selftest_default_kind` | `"short"` | `short` \| `long` (D10) | not implemented — the card offers both |
| `report_export_formats` | `["txt"]` | enabled export formats (D11) | not implemented — all four formats are always offered |

### `scan_history` — one row per scan run (MVP: scan-history log, FEATURES A13)

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | referenced by `snapshots.scan_id` |
| `started_at` / `ended_at` | INTEGER (unix ms) | |
| `trigger` | TEXT | `manual` \| `schedule` |
| `ok` | INTEGER | 0 when the scan failed outright |
| `error` | TEXT | failure reason, if any |
| `devices_total` / `devices_ok` / `devices_failed` | INTEGER | per-scan counts |

### `device_transport_overrides` (MVP: per-device `-d` override, FEATURES A11)

| Column | Type | Notes |
|---|---|---|
| `serial` | TEXT PK | |
| `smart_d` | TEXT | e.g. `sat`, `usbjmicron`, `usbcypress` |
| `updated_at` | INTEGER (unix ms) | |

### Acknowledgement — no table of its own

`ADR-017` proposed an `alert_acknowledgements` table. What was built (`B7`) is three columns on
`device_notes` — `ack_health`, `ack_note`, `ack_at`, added by migration `0009` — because there is
one acknowledgement per drive and it expires with the *verdict* rather than on a timer. `ack_health`
empty means "not acknowledged", so there is no third state to distinguish.

Maintenance windows deliberately get **no table**: they are a short JSON list in the
`maintenance_windows` setting, read once per dispatch pass and evaluated against the
clock. Nothing sleeps until a window opens or closes, so they add no idle cost
(`FEATURES.md` §E).

## Retention & pruning

- After every scan, snapshots older than `retention_days` (default 180) are deleted.
- `notification_log` rows older than 30 days are pruned in the same pass.
- Pruning is a single indexed DELETE per table; runs inline at end of scan (adds
  milliseconds), keeping the zero-idle-cost invariant.

## Migrations

Embedded numbered migrations (`migrations/0001_init.sql`, …) applied at startup inside
a transaction; a `schema_version` table (created automatically by the migrator)
tracks state. No runtime migration tooling — the app is the only writer.

## Concurrency

All writes happen from the single scan executor goroutine or from API handler writes
(user notes/schedule edits), serialized via SQLite's own write lock plus a coarse
application-level mutex around transactions. WAL mode allows reads (page loads) to
never block during scans.

## `knowledge_docs` (`A21`, migration `0011`)

The two *stored* layers of the drive knowledge base. The third — the table bundled with the binary —
is embedded in the executable and needs no storage.

| Column | Notes |
|---|---|
| `kind` | `cache` or `overrides`. **The primary key**, which is what keeps this at two rows. |
| `payload` | The JSON document, verbatim. |
| `source_url` | Where a fetched copy came from. Empty for overrides, which have no source. |
| `fetched_at` | When it was fetched, in milliseconds. `0` means "not fetched" — which is why a zero is *not* converted to a time, or every override would claim 1970. |
| `updated_at` | Last write. |

**Two rows in one table rather than two tables.** The fetched copy and the user's entries are the
*same kind of thing* — a knowledge document — so they are distinguished by a column rather than by
living in different places.

**The payload is stored verbatim rather than shredded into columns**, because the file *is* the
format: the editor hands the user a copy of it, the refresh stores what it fetched, and a
column-per-field schema would be a third representation to keep in step with the other two.

A layer that fails to parse is **skipped rather than fatal** when the base is resolved. The bundled
table is the floor, so a corrupt cached copy or an entry broken by a typo degrades to "we know less"
instead of "the drive page does not load". The editor is where a typo is refused, with the message in
front of the person who typed it.
