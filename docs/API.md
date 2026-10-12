# Smart-Serve — Internal API Specification

All endpoints JSON unless noted. Unauthenticated at MVP (see SECURITY notes in
`DESIGN.md` §7). Paths are relative to `SMARTSERVE_BASE_PATH` (default `/`,
`ADR-015`). State-changing requests (POST/PUT/DELETE) require a same-origin/CSRF
check. Error envelope: `{ "error": "message" }` with an appropriate HTTP status.
Timestamps are Unix milliseconds.

## Scan & devices

### `POST /api/scan` — trigger an on-demand scan

Runs the scan pipeline; single-flight (joins an in-flight scan). Returns `202` with
`{ "status": "started" }` (or `"joined"` when a scan is already running); the
`scan_id` arrives with the SSE `scan_complete` event — the client does not poll.

For scripts and tests, `POST /api/scan?wait=1` blocks until the scan finishes and
returns `{ "scan_id", "status", "devices_total" }` (status `completed` or `joined`).

The optional `trigger` query parameter is recorded in the scan history and decides
whether a failure is notification-worthy: a failed *scheduled* scan alerts, a failed
manual one does not (the operator who pressed the button already knows).

### `GET /api/devices` — list devices (last known state; never triggers I/O)

```json
{
  "devices": [
    {
      "serial": "S5GXNX0R512345J",
      "model": "Samsung SSD 870 EVO 1TB",
      "device_path": "/dev/sda",
      "transport": "sata",
      "model_family": null,
      "firmware": "SVT02B6Q",
      "interface_speed": "6.0 Gb/s",
      "capacity_bytes": 1000204886016,
      "health": "caution",
      "health_rationale": "5 pending sectors",
      "temperature_c": 41,
      "smart_available": true,
      "name": "boot",
      "notes": null,
      "hidden": false,
      "mountpoints": ["/"],
      "fstype": "ext4",
      "last_scan": 1730000000000,
      "is_missing": false
    }
  ],
  "last_scan_ts": 1730000000000
}
```

`mountpoints` / `fstype` are aggregated from the disk **and its partitions**: on Linux a
whole-disk node (`/dev/nvme0n1`) is never itself mounted, so a disk whose partitions are
mounted at `/` and `/home` reports `["/", "/home"]`.

### `GET /api/devices/{serial}` — full device detail

Everything above plus the normalized SMART payload for the last scan: ATA attribute
array (`id, name, value, worst, threshold, raw_value, raw_interpreted, verdict`) or
NVMe health block (`critical_warning, temperature, available_spare, percentage_used,
data_units_read, data_units_written, host_reads, host_writes, controller_busy_time,
power_cycles, power_on_hours, unsafe_shutdowns, media_errors, num_err_log_entries,
warning_temp_time, critical_comp_time`).

```json
{
  "device": { "...": "as above" },
  "snapshot": {
    "ts": "2025-10-12T10:17:20Z",
    "scan_id": 12,
    "health": "good",
    "score": 100,
    "temperature_c": 33,
    "power_on_hours": 31603,
    "wear_pct": 4,
    "attributes": { "attributes": [], "nvme": { "...": "..." } }
  },
  "metrics": {
    "power_on_hours": 31603,
    "power_cycles": 289,
    "data_read_bytes": 6322176000000,
    "data_written_bytes": 1200660480000,
    "life_left_pct": 94,
    "rotation_rate": 0
  }
}
```

`metrics` is derived server-side from the stored attributes so the UI never has to
understand ATA/NVMe layouts (see SMART_INTERPRETATION.md §9). Fields are omitted when
the drive does not report them; a drive with no S.M.A.R.T. data yields `{}`, and its
verdict stays `unknown`.

## User metadata

### `PUT /api/devices/{serial}/notes`

```json
{ "name": "boot", "notes": "bought 2024-03, under warranty until 2029", "tags": "critical,os", "hidden": false }
```

Creates the note row if absent (device may currently be unplugged). Response: the
stored record. Unset fields are `null`, not omitted — full replace semantics.

## Schedules

- `GET /api/schedules` — list (includes `last_run` and `next_run`).
- `POST /api/schedules` — create: `{ "kind": "interval", "every_seconds": 21600, "label": "every 6h" }`
  or `{ "kind": "cron", "cron": "0 3 * * *", "label": "nightly" }`. Cron is evaluated
  in the container's local timezone.
- `PUT /api/schedules/{id}` — update fields / `enabled` toggle.
- `DELETE /api/schedules/{id}` — remove.
- `POST /api/schedules/{id}/run` — fire once now. The in-process scheduler owns
  scan timing, so this brings the schedule's next run forward and wakes it rather
  than scanning inline; the run is recorded by the same path as a timed fire, so
  `last_run`/`next_run` stay honest. Returns `202 Accepted`.

Every schedule change (create, update, delete, run-now) wakes the scheduler, so it
takes effect immediately instead of at the next sleep boundary.

## Notification channels

- `GET /api/channels`
- `POST /api/channels` — `{ "type": "ntfy", "label": "alerts", "config": { "server": "https://ntfy.sh", "topic": "my-smart-alerts" }, "events": ["health_change", "temperature_high", "device_disappeared"] }`
  (webhook config: `{ "url": "...", "method": "POST", "headers": {...} }`).
  An empty or absent `events` array means "every event". The config is validated
  against the channel type's sender, so a missing topic or a malformed URL is a
  `400` at the point of editing.
- `PUT /api/channels/{id}` — update (same validation).
- `DELETE /api/channels/{id}`
- `POST /api/channels/{id}/test` — send a test event immediately, bypassing
  subscription filters and squelching (`NOTIFICATIONS.md`). Returns
  `{ "status": "sent", "channel_id": 1 }`; a delivery failure returns `502` with
  the destination's own error, which is the only way to tell a bad token from a
  bad URL. Without notification support wired in, it returns `501`.

### `GET /api/notifications` — delivery log

```json
{
  "notifications": [
    { "id": 3, "channel_id": 1, "event_type": "health_change",
      "event_key": "health_change:S5GXNX0R512345J:caution",
      "sent_at": "2025-10-12T03:00:04Z", "status": "sent", "detail": "" }
  ],
  "event_types": ["health_change", "temperature_high", "device_disappeared",
                  "device_appeared", "scan_failure"],
  "channel_types": ["ntfy", "webhook"]
}
```

`status` is `sent`, `failed` or `squelched`. The log is the only record of an alert
that fired while a channel was down, so it is the recovery path described in
ADR-008. `event_types` and `channel_types` are served here so the UI's subscription
checkboxes and type picker cannot drift from the server.

## Settings

- `GET /api/settings` / `PUT /api/settings` — key/value map. The full default key
  list (thresholds, retention, squelch, theme, excludes) is in `DATA_MODEL.md`;
  every interpretation threshold is edited here, never via env vars (`ADR-009`).
  The response includes each setting's effective value, its default, and its
  source (`env` or `db`). Appearance keys: `theme`, `theme_base`, `theme_accent`,
  `theme_surface_tint` (`UI.md` §5).

## Diagnostics & operations

- `GET /healthz` — `200 OK`, no I/O. Intended for the Docker `HEALTHCHECK` and
  orchestrator liveness probes.
- `GET /api/scans` — scan-history log (`FEATURES.md` A13): id, trigger
  (`manual`/`schedule`), start/end, duration, per-device ok/fail counts, error.
- `GET /api/devices/{serial}/raw` — raw `smartctl --json` (and the exact command
  line used) for the last scan; the diagnostics escape hatch for USB bridges
  (`FEATURES.md` A10).
- `PUT /api/devices/{serial}/transport` — set a per-device `-d` transport override
  (`{ "smart_d": "sat" }`), persisted in `device_transport_overrides`
  (`FEATURES.md` A11).
- `GET /api/devices/{serial}/report?format=txt|json|html` — CrystalDiskInfo-style
  report export for warranty/RMA (`FEATURES.md` A8). Enabled formats come from the
  `report_export_formats` setting; `?bundle=1` returns a zip of all enabled formats
  (Phase 2). **Implemented so far:** `txt` (default) and `json`; `html` and
  `?bundle=1` land with the Phase 2 export work.

## Phase 2 endpoints (specified now to freeze direction)

- `POST /api/devices/{serial}/selftest` — body `{ "kind": "short" | "long" }`;
  defaults to the `selftest_default_kind` setting (`short`); requires
  `selftest_enabled`. `GET …/selftest` returns the self-test log (`FEATURES.md` A19).
- `POST /api/acknowledge` — `{ "event_key": "health_change:<serial>:caution" }`
  mutes that event without changing the verdict (`FEATURES.md` B7).
- `GET|POST|PUT|DELETE /api/maintenance-windows` — silence windows
  (`FEATURES.md` B8).

## Events (SSE)

### `GET /api/events` — text/event-stream, long-lived

Event types pushed to connected browsers:

| Event | Payload |
|---|---|
| `scan_started` | `{ started_at }` |
| `scan_device` | `{ scan_id, device, phase: "start"\|"done", ok, error? }` |
| `scan_complete` | `{ scan_id, devices_total, devices_ok, devices_failed, duration_ms }` |
| `schedule_fired` | `{ schedule_id }` — a scheduled scan is about to start |
| `schedule_ran` | `{ schedule_id, scan_id, ok, next_run }` — it finished and the schedule advanced |
| `notify_error` | `{ error }` — the post-scan hook panicked; the scan itself is unaffected |

SSE is the only push mechanism; no WebSocket (see ADR-006). Reconnect handling is
native (browser `EventSource` auto-retry with `Last-Event-ID` cursor for missed
`scan_complete`s; the server keeps a small ring buffer of recent events).

## History (Phase 2 endpoint, specified now to freeze schema)

- `GET /api/devices/{serial}/history?from=…&to=…` — snapshots in range, with
  computed series (`temperature_c`, `wear_pct`, attr-5/197/198 raw values,
  `power_on_hours`).
