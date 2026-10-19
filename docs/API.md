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
      "bay": "bay 4",
      "notes": null,
      "hidden": false,
      "mountpoints": ["/"],
      "fstype": "ext4",
      "fs_version": "1.0",
      "removable": false,
      "smart_d": "sat",
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

`fs_version`, `removable` and `smart_d` describe drives that report no S.M.A.R.T. at all
(`FEATURES.md` A22). `removable` is omitted when lsblk did not say, so a client can tell
"fixed media" from "unknown"; `smart_d` is the remembered per-device `smartctl -d`
override, omitted when none is set.

`smart_cause` is present only when `smart_available` is false, and names *why* the read
failed. Three unrelated problems arrive from smartctl as the same "Operation not
permitted", and they need three different fixes, so the app classifies them rather than
passing the text through (`internal/smart/failure.go`):

| `smart_cause` | Means | Fix |
|---|---|---|
| `device_blocked` | the device node could not be opened | `device_cgroup_rules: ['a *:* rwm']` |
| `query_blocked` | it opened, but the query was refused | `cap_add: [SYS_ADMIN, SYS_RAWIO]` |
| `unsupported` | the drive has no S.M.A.R.T. to report | none — this is a USB flash drive |
| `missing` | the node is gone | replug the drive, or rescan |

The first two are the container's problem rather than the drive's, which is why they are
distinguished: telling a container user their drive "does not report S.M.A.R.T." when it
was simply not allowed to be read sends them looking in the wrong place.

`worsening` appears on the **list** endpoint only, and only when it is non-zero: it is
the number of bad sectors that appeared over the recent window, computed for every drive
in one query so the dashboard can flag a drive that is going bad without the user having
to open each one. A drive with fewer than two snapshots in the window is absent rather
than reported as flat — no history is not the same finding as a clean bill of health.

`power_on_hours` and `wear_pct` also appear on the **list** endpoint, taken from each
drive's newest snapshot. They live on snapshots rather than on `devices` because they are
readings rather than current state, and they are on the list so the dense fleet table
(`FEATURES.md` D8) can show a drive's age per row without opening every drive to find it.
One query for the whole list, keyed on the highest snapshot id rather than the highest
timestamp — two snapshots can share a millisecond when a partial rescan follows a full
one, and "the latest" has to be one row.

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
  },
  "projection": {
    "basis": "wear",
    "summary": "94% of rated life left, about 2.5 years at the current rate",
    "confidence": "medium",
    "caveat": "Extrapolated from the observed rate over this window. Wear is rarely linear, and the rating is the manufacturer's, not a measurement."
  }
}
```

`metrics` is derived server-side from the stored attributes so the UI never has to
understand ATA/NVMe layouts (see SMART_INTERPRETATION.md §9). Fields are omitted when
the drive does not report them; a drive with no S.M.A.R.T. data yields `{}`, and its
verdict stays `unknown`.

`device.logs` summarises the drive's **own** logs (`FEATURES.md` A20). It appears only on this
endpoint — the list response omits it, so a dashboard refresh does not parse every drive's
stored document — and only when the drive reported something. An **absent** field means the
drive said nothing, which is a different finding from an empty log:

```json
"logs": {
  "error_count": 1,
  "self_test_count": 2,
  "self_test_supported": true,
  "short_minutes": 2,
  "extended_minutes": 10,
  "error_entries": [ { "number": 1, "index": 0 } ],
  "self_test_entries": [
    { "type": "Short offline", "status": "Completed without error", "passed": true, "hours": 2116 },
    { "type": "Short offline", "status": "Self-test routine in progress", "remaining": 50, "hours": 2116 }
  ]
}
```

A count of `0` is a finding and is present. `note` carries smartctl's own words when a log could
not be read — an NVMe drive with no self-test log answers the request with an error and reports
nothing else about it. `self_test_supported: false` means the drive cannot run self-tests, or
cannot have its self-test log read.

The two entry lists differ because the two logs do. A **self-test** entry carries the test, its
outcome, and `remaining` while one is running — the progress figure a running test has to show,
since a spinner with no percentage is indistinguishable from a hang. Its `passed` field is absent
while a test is in progress, and that is deliberately not `false`: an absent verdict is not a
failure. An **error** entry may carry nothing but `number` and `index`, because on a drive smartctl
does not recognise that is all the drive reports, and the panel shows a table only when an entry
has more to say than that.

Everything here is parsed from the raw document already stored for the device, so no extra device
access happens and the idle-cost invariant is untouched.

`projection` is what the drive's **history** says about where it is heading, over a
180-day window (`internal/interp/longevity.go`, `SCOPE.md` §2.2). The window matches the
default `retention_days` setting, since snapshots older than that have been pruned; lowering
retention shortens the window, which is correct — the trend is limited by the history that
exists rather than by the constant.

| Field | Meaning |
|---|---|
| `basis` | `wear` (the drive's own life counter), `sectors` (bad-sector growth), or `none` |
| `summary` | the one-line answer |
| `confidence` | `low`, `medium`, or absent when there is no basis |
| `caveat` | what the number is not — always present with a summary |
| `worsening` | present only when something is measurably getting worse, and says what |

Two deliberate limits. A sector **rate is never turned into a date**: sector counts
arrive in bursts, and a date extrapolated through two of them looks precise and means
nothing. And ATA wear counters are not interpreted at all — they are vendor-specific
enough that guessing is worse than reporting none — so an ATA drive falls back to the
sector basis. A drive with less than a day of history gets `basis: "none"` and
"Not enough history yet", because two scans an hour apart carry no rate worth having.

## User metadata

### `PUT /api/devices/{serial}/notes`

```json
{ "name": "boot", "bay": "bay 4", "notes": "bought 2024-03, under warranty until 2029", "tags": "critical,os", "hidden": false }
```

Creates the note row if absent (device may currently be unplugged). Response: the
stored record. Unset fields are `null`, not omitted — full replace semantics.

`bay` is free text: where the drive physically is. It is user metadata rather than
something read from the drive, so it survives an unplug, and it is what the dashboard
card leads with when it is set (`FEATURES.md` D10).

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
- `POST /api/channels` — `{ "type": "ntfy", "label": "alerts", "config": { "server": "https://ntfy.sh", "topic": "my-smart-alerts" }, "events": ["health_change", "temperature_high", "device_disappeared"] }`.
  An empty or absent `events` array means "every event". The config is validated
  against the channel type's sender, so a missing topic or a malformed URL is a
  `400` at the point of editing. Config shapes per type are in `NOTIFICATIONS.md`.
- `PUT /api/channels/{id}` — update (same validation).
- `DELETE /api/channels/{id}`
- `POST /api/channels/{id}/test` — send a test event immediately, bypassing
  subscription filters and squelching (`NOTIFICATIONS.md`). Returns
  `{ "status": "sent", "channel_id": 1 }`; a delivery failure returns `502` with
  the destination's own error, which is the only way to tell a bad token from a
  bad URL. Without notification support wired in, it returns `501`.
- `POST /api/channels/test` — the same, for a channel described in the body rather than
  a stored one (`{ "type": ..., "config": ... }`, same shape and validation as
  `POST /api/channels`). The editor uses it so **Test** proves what is on screen,
  including edits that have not been saved, and so a failed test can be corrected and
  retried without the dialog closing. Returns `{ "status": "sent" }`; nothing is
  persisted, and because the draft has no id it is not written to the delivery log
  either.

### Channel objects and environment overrides

A channel object carries two fields beyond its stored configuration (`ADR-022`):

```json
{
  "id": 1, "type": "ntfy", "label": "Critical alerts",
  "config": { "server": "https://ntfy.sh", "topic": "alerts" },
  "enabled": true, "events": ["health_change"],
  "env_prefix": "SMARTSERVE_CHANNEL_CRITICAL_ALERTS",
  "sources": { "token": "environment" }
}
```

- `env_prefix` is the prefix this channel's fields are overridden by, derived from its label.
  It is returned so the editor can *show* the variable names instead of leaving them to be
  guessed, and so renaming a channel reads as a rename.
- `sources` names, per field, where the effective value comes from: `environment`, `file` or
  `database`. Fields with nothing to report are absent.

`sources` deliberately carries **provenance and never values**. Handing a resolved secret to a
browser would leak it into the page, and the next `PUT` would write it into the database —
promoting a secret that was meant to stay out of it.

Validation uses the *resolved* config, not the submitted one, so a channel whose token comes
from the environment can be saved with the token field left blank.

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
  "channel_types": ["discord", "gotify", "ntfy", "smtp", "telegram", "webhook"],
  "channel_env_vars": ["SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN"]
}
```

`status` is `sent`, `failed` or `squelched`. The log is the only record of an alert
that fired while a channel was down, so it is the recovery path described in
ADR-008. `event_types` and `channel_types` are served here so the UI's subscription
checkboxes and type picker cannot drift from the server. `channel_env_vars` lists the names of
the `SMARTSERVE_CHANNEL_*` variables this deployment has set — **names only, never values** —
so the editor can badge a field the deployment supplies rather than showing it blank, and can
stop demanding a value that is never going to be typed into the form (`ADR-022`).

## Settings

- `GET /api/settings` / `PUT /api/settings` — key/value map. The full default key
  list (thresholds, retention, squelch, maintenance windows, theme) is in
  `DATA_MODEL.md`; every interpretation threshold is edited here, never via env vars
  (`ADR-009`). The response includes each setting's effective value, its default,
  and its source (`env` or `db`). Appearance keys: `theme`, `theme_base`,
  `theme_accent`, `theme_surface_tint` (`UI.md` §5). `maintenance_windows` is the
  JSON list described in `NOTIFICATIONS.md`; it has no endpoint of its own.

## Diagnostics & operations

- `GET /healthz` — `200 OK`, no I/O. Intended for the Docker `HEALTHCHECK` and
  orchestrator liveness probes.
- `GET /api/scans` — scan-history log (`FEATURES.md` A13): id, trigger
  (`manual`/`schedule`), start/end, duration, per-device ok/fail counts, error.
- `GET /api/devices/{serial}/raw` — raw `smartctl --json` (and the exact command
  line used) from the last scan; the diagnostics escape hatch for USB bridges
  (`FEATURES.md` A10). Served from the stored per-device output, so it survives a
  restart and is unaffected by rescanning a different drive; `404` when nothing has
  been stored yet, with a message saying to rescan.
- `PUT /api/devices/{serial}/transport` — set a per-device `-d` transport override
  (`{ "smart_d": "sat" }`), persisted in `device_transport_overrides`
  (`FEATURES.md` A11).
- `POST /api/devices/{serial}/scan` — rescan **only this drive**, synchronously.
  Returns `{ "scan_id": 2, "status": "completed", "serial": "…", "devices_total": 1 }`.
  A `404` means the drive is not connected any more. This is what makes the
  per-card Scan button honest: it used to run a full scan, so pressing it on one
  card re-read every drive.
  A single-drive scan deliberately does **not** mark the drives it did not look at
  as disconnected (`DATA_MODEL.md`); they were never checked.
- `DELETE /api/devices/{serial}` — **forget** a drive: the device row, its snapshots
  and its notes. `scan_history` is left alone, because it records what happened
  rather than what exists. Returns `{ "serial": "…", "forgotten": true }`, or `404`
  if the serial was never stored. A later scan re-adds the drive if it is physically
  present — forgetting is not a blocklist.
- `GET /api/devices/{serial}/report?format=txt|json|html|csv` — report export for
  warranty/RMA (`FEATURES.md` A8). `txt` (default), `html` and `csv` are served as
  downloads with a `smart-serve-<serial>.<ext>` filename; `json` is returned inline
  because it is the machine format. An unknown format is a `400`.
  `txt` is a CrystalDiskInfo-style summary, `html` a self-contained page with no
  external references (it has to stay readable after the app is uninstalled), `csv`
  a two-column `field,value` sheet covering identity, health and every attribute.
  A per-format enable list (`report_export_formats`) and a `?bundle=1` zip remain
  unimplemented (`DATA_MODEL.md`).
- `GET /api/devices/{serial}/selftest` — the drive's self-test state, read **live**
  rather than from the stored document, because a test's progress exists nowhere the
  app stores. Returns `{ logs, running }`: `logs` in the same shape the device detail
  uses, so the UI renders it with the function it already has, and `running` the entry
  for a test in progress, present only while one is. `running` is computed server-side
  so the rule for recognising a test lives in one place — and that rule is the
  percentage remaining, not `passed`, because a real drive reports `passed: true` for a
  test that is still going. This is the only endpoint that reads a device outside a
  scan (`FEATURES.md` §E).
- `POST /api/devices/{serial}/selftest` — body `{ "kind": "short" | "long" }`. Starts a
  test, and is **the only request in this API that changes a drive.** A `400` for a
  `kind` that is neither of the two, a `409` when no device path is recorded, and a
  `502` carrying smartctl's own words when the drive refuses. The response quotes the
  drive's acknowledgement in `note` rather than paraphrasing it.
- `POST /api/devices/{serial}/selftest/abort` — stops whichever test is running
  (`smartctl -X`). A long test runs for hours, so this is a first-class action rather
  than a footnote.

  For all three, success is decided by smartctl's **exit status**, not by its text, and
  only the low three bits count: an option it could not parse, a device it could not
  open, a SMART command the drive refused. The remaining bits describe the *drive* — it
  is failing, it has a past failure, its logs hold errors — and reading those as a
  command failure would report a self-test that ran perfectly as broken, on exactly the
  drives that most need one.

## Phase 2 endpoints (specified now to freeze direction)

- `POST /api/acknowledge` — `{ "event_key": "health_change:<serial>:caution" }`
  mutes that event without changing the verdict (`FEATURES.md` B7).

Maintenance windows deliberately get **no endpoints**: they are a `maintenance_windows`
setting (`DATA_MODEL.md`) edited from the Notifications page, because a window is a
configuration value rather than a resource with its own lifecycle.
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

## History (implemented)

- `GET /api/devices/{serial}/history?from=…&to=…` — snapshots in range, with
  computed series (`temperature_c`, `wear_pct`, attr-5/197/198 raw values,
  `power_on_hours`).
