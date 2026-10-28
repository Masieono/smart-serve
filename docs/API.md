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
      "tags": "tank, media",
      "hidden": false,
      "on_bench": false,
      "mountpoints": ["/"],
      "fstype": "ext4",
      "fs_version": "1.0",
      "removable": false,
      "smart_d": "sat",
      "last_scan": 1730000000000,
      "is_missing": false
    }
  ],
  "ranking": [{ "serial": "S5GXNX0R512345J", "band": "watch", "reasons": ["5 pending sectors"] }],
  "bench_tag": "bench",
  "last_scan_ts": 1730000000000
}
```

`ranking` is the replace-first shortlist (`FEATURES.md` A24), and `bench_tag` names the tag this
app treats as "on the bench" (`D11`). Both are sent rather than derived by the client, so the UI
cannot disagree with the server about which drive to buy or which drive is being tested.

`correlation` is the group-heat finding (`A25`): the tag groups whose drives all rose together,
which points at what they share rather than at the drives. It is computed from the store's own two
readings per drive rather than from the payloads above, because those carry only the latest one.

```json
"correlation": [
  { "group": "tank", "drives": ["S1", "S2", "S3", "S4"], "total": 4, "delta_c": 8, "max_c": 47 }
]
```

`drives` are the serials that moved and `total` is the size of the group they moved within — the
ratio is carried rather than reduced because six of six is a different finding from six of
twenty-four, and that judgement belongs to the reader. `delta_c` is the **smallest** rise among the
drives named, so the figure is true of every one of them; a mean would let one drive's spike stand
in for the others. The thresholds behind it are settings (`DATA_MODEL.md`), and it is **empty**
rather than absent when nothing is moving — the same "no finding" shape as an empty `ranking`.

A bench drive is left out of this too (`D11`): a drive being tested is expected to heat up, and a
batch of them on one bench would fire it on every scan.

`on_bench` is the server's reading of that tag for one drive, so a client never has to know the
tag's name or split a tag string the way the grouping does. It is derived from `tags` on every
request rather than stored, because the tag is the source of truth and a second column could
disagree with it. A bench drive is left out of `ranking` and out of the health counts the UI
derives; it is **never** left out of this list or of an export.

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

`ranking` is the **"replace first"** order (`FEATURES.md` A24), served alongside `devices` so the
shortlist and the cards cannot disagree about a drive. It answers a different question from the
dashboard's own sort: "which one do I buy this month?" is about direction and remaining life as much
as about today's verdict.

```json
"ranking": [
  { "serial": "S1", "name": "parity", "band": "now",
    "reasons": ["5 reallocated sectors", "94% of its rated write endurance used"] }
]
```

`band` is `now`, `soon`, `watch` or `fine`, ordered worst first. It is a **band rather than a
score**, deliberately: a weighted sum would be a number nobody could argue with or explain, and the
point of the ranking is that it shows its reasons. Every drive is returned, including the `fine`
ones, so the caller decides how much to show. A **disconnected** drive is not included — its stored
verdict is the last one ever *seen* rather than a reading anyone just took, and ranking it would put
a backup drive someone unplugged on purpose above a drive that is actually failing.


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

`bench_tag` is the tag this app treats as "on the bench" (`FEATURES.md` D11). Sent here as well as
with the list, so the notes editor can offer it as a **checkbox** rather than making the reader know
the name and type it — the contract stays on the server either way.

```json
{
  "device": { "...": "as above" },
  "bench_tag": "bench",
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

`device.changes` is what moved between the last two scans (`FEATURES.md` A18). Also detail-only,
for the same reason as `logs`: it needs two snapshots, and the list has no place to put the answer.

```json
"changes": {
  "from": "2025-10-12T04:00:00Z",
  "to": "2025-10-12T10:17:20Z",
  "health": { "from": "good", "to": "caution" },
  "score": { "from": 96, "to": 74 },
  "temperature_c": { "to": 41 },
  "sectors": { "from": 0, "to": 5 },
  "attributes": [
    { "id": 5, "name": "Reallocated_Sector_Ct", "from": 0, "to": 5, "value": 100, "worst": 100,
      "threshold": 10, "trouble": true }
  ],
  "routine": 4,
  "note": "Only one scan has been recorded for this drive..."
}
```

An **absent** end is a reading the drive did not give, not a zero: the example's temperature has no
`from` because the previous scan did not report one. `attributes` holds only the movements the app
treats as a finding — the drive's *own* threshold being met, or one of the lost-storage counters —
and `routine` counts the rest rather than dropping them, because a silent filter would be a lie by
omission. `appeared` marks a counter the drive has just started reporting. `note` is present when
there is no diff to make at all, which is a different finding from an empty one.

`projection` is what the drive's **history** says about where it is heading, over a window that is
the user's own `retention_days` setting (`internal/store/settings.go`, `FEATURES.md` A26). It used
to be fixed at 180 days, which meant an owner who raised retention to watch a slow trend had that
history discarded by the projection anyway; the window is now the same number the pruning uses, so
what the user keeps is what the app reads. The default is 180 days, and an unparseable or
non-positive setting falls back to it rather than meaning "keep nothing".

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

- `PUT /api/devices/{serial}/ack` — **acknowledge the drive's current condition** (`FEATURES.md` B7),
  body `{ "note": "RMA raised" }`. Returns `{ "serial": "…", "ack": { "health": "caution", "note": "…",
  "at": "…" } }`.

  It silences the alerts that describe the *condition* — `health_change` and `temperature_high` — and
  changes no finding: the verdict still reads as caution, because the drive is caution. What it
  removes is the repetition.

  **The acknowledged verdict comes from the stored device, not from the request**, because the ack has
  to name the condition the *server* last assessed. A stale client could otherwise acknowledge a
  verdict the drive no longer has and silence a real change.

  A `400` on a drive that is reporting nothing wrong: an ack on a healthy drive would sit there waiting
  to silence the first problem it develops, which is the opposite of what the user meant. A `404` for
  an unknown serial.
- `DELETE /api/devices/{serial}/ack` — remove the acknowledgement, so the drive alerts again on the
  next scan. Same response shape, with `"ack": null`.

  The ack is **spent the moment the verdict moves**: a drive acknowledged at `caution` that goes `bad`
  alerts normally. That is a better expiry than a timer — it is exactly the condition the user named,
  and it happens when there is something new to say — which is why an ack has no duration.

Creates the note row if absent (device may currently be unplugged). Response: the
stored record. Unset fields are `null`, not omitted — full replace semantics.

`bay` is free text: where the drive physically is. It is user metadata rather than
something read from the drive, so it survives an unplug, and it is what the dashboard
card leads with when it is set (`FEATURES.md` D10).

## Prometheus metrics

- `GET /metrics` — the Prometheus text exposition format (`FEATURES.md` C6). **Off by default**: with
  `prometheus_enabled` unset or `false` it answers **`404`, not `403`** — the honest answer to "is
  this feature on?" is "it is not here", and a `403` would advertise an endpoint the operator did not
  enable.

  The idle-cost argument is not that the handler is expensive — it does no work of its own and runs
  no timer — but that enabling it invites *someone else's* scraper to poll this app on a schedule.

```text
smartserve_build_info{version="1.0.2"} 1
smartserve_device_info{health="caution",model="WDC WD40EFRX",serial="S1",transport="sata"} 1
smartserve_device_score{serial="S1"} 74
smartserve_device_temperature_celsius{serial="S1"} 41
smartserve_device_power_on_hours{serial="S1"} 31603
smartserve_device_wear_percent{serial="S1"} 94
smartserve_device_new_sectors{serial="S1"} 7
smartserve_devices_total 4
smartserve_devices{health="bad"} 1
smartserve_devices{health="caution"} 1
smartserve_devices{health="good"} 2
smartserve_scan_last_success_timestamp_seconds 1760000000
```

Three rules shape it, and they are the same three the rest of the app follows.

**A reading the drive did not report drops the series rather than exporting a zero.** A `0` is a
claim that the drive reported zero; an absent series is "no measurement", which is both what
Prometheus expects and what the em dash means on screen. A drive that could not be scored has no
`score` series, and a count that is not moving has no `new_sectors` series.

**Descriptive labels live in one `_info` metric**, with the numeric series keyed by `serial` alone. A
model, a bay and a tag set are constants per drive, so repeating them across five metrics would
multiply the series count for nothing.

**Unplugged drives are still described and still counted.** They are part of the inventory, and their
readings are the last ones anybody saw — the same reason the fleet export includes them.

The format is written by hand rather than with `prometheus/client_golang`: the exposition format is
four rules and a handful of escaping, this is six metrics, and the library would multiply a dependency
tree that currently holds exactly one module (`ADR-020`).

## Import and export

- `GET /api/config/export` — the deployment's **labels, settings and schedules** as one JSON file
  (`FEATURES.md` C7), served as a download. The point of it is a host migration: names, bays and
  tags live in the database, which survives a restart but not a move.
- `POST /api/config/import` — the same shape back. **Merges**, with the file winning for what it
  contains: a drive or a setting the file does not mention is left alone, because a restore that
  deleted anything it did not recognise would be one you could not safely run twice. Schedules are
  the exception — a file that carries them *replaces* the list, since a schedule has no natural key
  and importing the same file twice would otherwise leave duplicates. An absent `schedules` key
  leaves them alone; an empty array means "none". Returns `{"applied": {"devices": n, "settings": n,
  "schedules": n}}`.

```json
{
  "smartserve": 1,
  "exported_at": "2025-10-12T10:17:20Z",
  "note": "Device labels, settings and schedules. Channels are deliberately not included...",
  "devices": [{ "serial": "S1", "name": "parity", "bay": "bay 4", "tags": "tank", "notes": "..." }],
  "settings": { "retention_days": "365" },
  "schedules": [{ "kind": "interval", "every_seconds": 21600, "label": "every 6h", "enabled": true }]
}
```

`smartserve` is a format marker and is **checked**: handing this endpoint the wrong JSON file says
so, rather than applying nothing and reporting success.

**Channels are deliberately absent.** Their config holds tokens and passwords, and a backup file is
exactly the kind of thing that gets emailed to yourself — the app already refuses to serve secret
values over the API, and a backup that quietly undid that would be a worse trade than the one it
saves. The bundle says so in its own `note`, so a file found later explains itself.

## Schedules

- `GET /api/schedules` — every schedule. Each carries `kind` (*when* it runs: `interval` or `cron`)
  and `action` (*what* happens: `scan`, `digest` or `selftest`, `FEATURES.md` B13, A19).
  The response also carries `selftest_pass` — `{running, done, total, current}` — while a scheduled
  self-test pass is draining, so the page can say what it is doing rather than leaving the reader to
  infer it from a `last_run` that has already passed.
- `POST /api/schedules` / `PUT /api/schedules/{id}` — body
  `{ "label": "weekly", "kind": "cron", "cron": "0 9 * * 1", "action": "digest", "enabled": true }`.
  An omitted or empty `action` means `scan`, which is what every schedule did before `B13`; anything
  else is a `400`, because a schedule that quietly does nothing when it fires is worse than one that
  refuses to be created.

  `action: digest` sends **one summary of the period since this schedule last ran** instead of
  scanning — see `NOTIFICATIONS.md` for what is in it. A digest schedule that has never run summarises
  the last seven days.

  `action: selftest` runs a self-test on **every drive that can take one**, one drive at a time in
  serial order, and carries `selftest_kind` (`short` or `long`, default `short`). It is the only
  schedule that writes to a drive (`ADR-024`). The fire queues a pass and returns: a pass takes hours,
  so it is not bounded by the scan timeout, and a fire that arrives while one is still draining is
  logged and skipped rather than queued twice. Each drive is re-read as its test finishes, so the
  result is stored and notified by the same path a manual test uses.

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
  warranty/RMA (`FEATURES.md` A8, `A27`). `txt` (default), `html` and `csv` are served as
  downloads with a `smart-serve-<serial>.<ext>` filename; `json` is returned inline
  because it is the machine format. An unknown format is a `400`.
  `txt` is a CrystalDiskInfo-style summary, `html` a self-contained page with no
  external references (it has to stay readable after the app is uninstalled), `csv`
  a two-column `field,value` sheet covering identity, health and every attribute.
  A per-format enable list (`report_export_formats`) and a `?bundle=1` zip remain
  unimplemented (`DATA_MODEL.md`).

  **Every format says what it is for** (`A27`), in the file rather than only in this
  document: it is read by whoever the file is forwarded to, and a file found in a folder a
  year later has to explain itself. It claims evidence, not proof — whether a S.M.A.R.T.
  report is a warranty case is the vendor's call.

  **Every format also names the evidence**, in two groups. *Failing attributes* are the
  attributes at or below the threshold the drive's **own manufacturer** set — the vendor's
  limit, in the drive's own terms, which is what an RMA argument turns on. A threshold of
  zero means "no threshold", not "must be zero", so those attributes are not listed. Because
  this is a listing rather than a verdict, it cannot contradict the stored verdict when a
  threshold changes between the scan and the export. *Evidence counters* are the attributes
  the app reads when it assesses a drive (`5`, `10`, `187`, `196`, `197`, `198`, `199`) whose
  raw value is non-zero, read through the interpreter itself so the report cannot cite a
  different set of attributes, or read the same value differently, than the verdict did. A
  group with nothing in it is omitted, because "nothing here" and "we did not look" are
  different findings.
- `GET /api/devices/export?format=csv|html&tag=<tag>` — the whole fleet as one file
  (`FEATURES.md` A23), served as a download named `smart-serve-fleet[-<tag>]-<date>.<ext>`.
  **One row per drive, not one row per attribute**: a spreadsheet's job here is to compare
  drives with each other, and the per-attribute sheet is the report above. Both formats
  render from the same column list and the same rows the dashboard shows, through the same
  ranking (`A24`), so the export cannot disagree with the screen.

  `csv` (default) carries a short preamble above the header — a title and what the file is —
  because a CSV in a shared folder is read months later by someone with no memory of where it
  came from. The cost is that a strict parser sees those rows; the machine-readable form is
  `GET /api/devices`, which already exists. `html` is a self-contained printable page, which
  is what gets attached when several drives are going back at once. There is deliberately no
  `json` format: `/api/devices` already is one, and a second shape would be a second thing to
  keep in step. An unknown format is a `400`.

  Three rules are worth knowing before parsing it. **An absent reading is an empty cell, never
  a zero** — a blank is what a spreadsheet's `AVERAGE` skips, while a `0` drags every average
  down and reads as a measurement. **The export is never filtered**: not by the dashboard's
  filter, not by `hidden`, because a wrong number gets noticed and a missing row does not;
  `Hidden` and `Connected` are columns so the reader can see why a drive was not on screen.
  And **the rows are in the ranking's own order** (`A24`), because that is the one thing a
  reader cannot recover from the file — sorting the `Replace` column alphabetically gives
  fine, now, soon, watch. Disconnected drives are listed, unranked, and last: the ranking
  excludes them by design, and their `Replace` cell is **empty rather than `fine`**, because
  `fine` is a claim the ranking did not make about them.

  `tag` is the one scope, and it is a **deliberate** one: a request that says "the batch" is
  not the same as view state the caller did not ask for and would not notice. It is how a
  bench batch gets its own sheet (`D11`), matched case-insensitively and by whole tag, so
  `benchtop` is not `bench`. A tag nothing carries is an empty sheet rather than a `404`:
  "the batch is empty" is a real answer, and a `404` would send the reader looking for a
  fault that is not there.

  The route shadows a device whose serial is literally `export`. Go's mux resolves a literal
  segment ahead of a wildcard, so the only cost is that such a serial is unreachable, and no
  drive has one.
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

## Knowledge base (implemented, `A21`)

The drive knowledge base: rated endurance and known firmware advisories, in three layers — bundled,
fetched, and yours — with later layers winning. The format is documented in
[`KNOWLEDGE.md`](KNOWLEDGE.md), which is the whole interface: the bundled file, the fetched copy and
the entries you type are all the same document.

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/knowledge` | The Settings view: the refresh URL, entry counts per layer, the resolved table, and the shipped document as text. |
| `POST` | `/api/knowledge/refresh` | Fetch the configured URL and store it. **A button, never a timer.** |
| `PUT` | `/api/knowledge/overrides` | Validate and store your entries. An empty `payload` clears the layer. |
| `DELETE` | `/api/knowledge/cache` | Discard the fetched copy, falling back to the shipped table. |

**The refresh is parsed before it is stored**, so a bad file leaves the previous layer in place rather
than replacing something readable with something that is not. Failures are reported with the URL and
the reason: `400` when the document is unusable, `502` when the URL answers with an error or not at
all. A URL that is not `http(s)` is refused with `400` before anything is fetched.

`PUT /api/knowledge/overrides` returns the **parse error verbatim**, naming the entry and the field —
the message goes back to the person who typed it, which is the whole reason the editor exists rather
than a second file to edit by hand.

`GET /api/devices` and `GET /api/devices/{serial}` carry a `knowledge` object when the table knows the
drive: `{ family, rated_tbw, rated_tbw_approx, rated_workload_tb_per_year, source, notes, advisories }`.
Absent when nothing matched, which is a real answer — the app says nothing rather than inventing a
figure. The list resolves the table **once per request**, not once per drive.

The user's own entries also travel in `GET /api/config/export` as a `knowledge` string, verbatim, and
`POST /api/config/import` validates and applies them. The fetched copy is deliberately **not** in the
bundle: it is a cache of a URL the settings already carry.
