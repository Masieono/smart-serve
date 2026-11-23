# Smart-Serve: Architecture & Design

## 1. Overview

Smart-Serve is a single Go binary packaged with `smartmontools` into a small Docker image. It
discovers storage devices, reads their S.M.A.R.T. data, interprets it into health verdicts, keeps
snapshots and user notes, and notifies external services when a drive's health changes.

Design goals, in priority order:

1. **Coverage.** Every disk in `lsblk` appears in the UI, including USB drives.
2. **Idle frugality.** ~0% CPU and a few MB of RAM when not scanning or serving a page.
3. **Self-containment.** One container, one port, one data volume, no host setup.
4. **Clarity.** CrystalDiskInfo-grade readable reporting, not raw S.M.A.R.T. dumps.

Non-goals: RAID-controller S.M.A.R.T. pass-through, multi-user accounts, and any write to a drive
other than starting a self-test the user asked for (Section 7).

## 2. Process model

One process, one binary, with the work in packages under `internal/`: `scan`, `smart`, `interp`,
`store`, `sched`, `selftest`, `notify`, `events`, `httpapi`, `webui`, `auth`, `knowledge`, `model`.

```
┌──────────────────────────────────────────────────────────┐
│                    smart-serve (Go)                      │
│  HTTP server ──► handlers ──► scan executor ──► smartctl │
│      │                        │       lsblk              │
│      │                   SQLite (/data/smartserve.db)    │
│      ├──► scheduler (sleeps until the next due scan)     │
│      └──► SSE hub ◄────────────┘                         │
│                        notifier bus ──► ntfy / webhook   │
└──────────────────────────────────────────────────────────┘
```

- **HTTP server** (stdlib `net/http`): the embedded UI and the JSON API on one port.
- **Scan executor:** discovery plus the S.M.A.R.T. pipeline (Section 3), serialized behind a mutex;
  a second request joins the running scan. **Scheduler:** one goroutine sleeping until the next due
  schedule, which runs a scan, a digest or a self-test pass (`docs/DATA_MODEL.md`).
- **Notifier bus:** publishes scan events to configured channels after each scan, with no timers and
  no loops; SSE progress goes only to connected browsers.

### 2.1 Idle-resource budget (a design invariant)

| Condition | CPU | RAM |
|---|---|---|
| Idle, no browser connected | ~0% | ~8-12 MB (binary plus SQLite page cache) |
| Idle, browser tab open | ~0% (SSE is epoll-driven) | +~1 MB per connection |
| Active scan (N devices) | one `smartctl` child at a time, sequential | +~5 MB per child |

Enforcement: no background device polling, no per-second timers, and the only goroutines between
events are the scheduler's `time.Sleep` and the HTTP accept loop. Discovery happens **only** inside an
explicit or scheduled scan; a page load reads the snapshot and never spawns `smartctl`. The one poll
in the product is the browser watching a running self-test, and it stops when the test ends.

## 3. Scan pipeline

Three stages. Every external command runs with a hard timeout (15 s by default).

### Stage 1: discovery

1. `smartctl --scan-open --json`, which returns the devices smartmontools can open and the transport
   hint some USB bridges need.
2. `lsblk --json --paths` for what the host sees: name, model, serial, size, transport, mountpoints,
   filesystem and WWN.

The **lsblk list is the source of truth** for coverage: if a disk is in lsblk, it is in the UI. Loop
devices and partitions are filtered out, and a disk absent from smartctl's scan is still listed, with
the error that explains it (Section 5).

### Stage 2: per-device query

Each device in sequence, never in parallel, to avoid I/O contention. `-x` rather than `-a`, because it
carries the vendor and NVMe log pages the interpreter needs.

- ATA/SATA/USB: `smartctl -x --json <dev>`, with the transport hint when one was discovered.
- NVMe: `smartctl -x --json /dev/nvme0`, with the per-namespace data smartctl reports.

### Stage 3: interpret and persist

1. Normalize the raw JSON into a device record (attribute table, NVMe health block, temperature,
   overall health) and compute the verdict per `docs/SMART_INTERPRETATION.md`.
2. Write a snapshot row (normalized data plus JSON) and upsert the device, pruning per retention.
3. Publish events to the notifier bus (`docs/NOTIFICATIONS.md`) and emit SSE progress throughout, so
   the UI's scan button has live feedback.

### 3.1 Single flight

The executor exposes one entry point guarded by a mutex and a condition variable: pressing Scan while
a scheduled scan runs joins it, pressing twice returns the same in-flight result, and the UI shows one
scanning state.

## 4. Frontend

Embedded with `embed.FS`, so there is no CDN, no Node build step and no framework.

- **Stack:** a server-rendered HTML shell (`index.html`, carrying the theme attributes), vanilla ES
  modules under `assets/js`, and three stylesheets. JavaScript talks to the backend through the JSON
  API and one SSE connection (`GET /api/events`).
- **One UI, five pages:** Fleet (`/`), Device (`/device/<serial>`), Schedules, Notifications and
  Settings with its four sub-pages, in a sidebar shell with a global scan and, when the optional
  sign-in layer is on, a sign-out control (`docs/UI.md`).
- **State:** no client-side store. Each view fetches its own payload, renders from it and re-renders
  on an SSE event, holding its container's height so the scroll position survives.
- **Freshness:** a page load never triggers a scan, views render the last snapshot, and opening a
  drive re-reads it only when its data is older than `device_refresh_minutes`.

## 5. USB and bridge strategy

- USB drives are the original motivation for the project, and `smartctl --scan-open` already tries the
  common SAT path, which covers most USB-SATA and USB-NVMe bridges; its hint is used for the read.
- There is no probing chain. When a bridge needs a `-d` value smartmontools did not guess, the user
  sets a **transport override** on the device page, remembered per serial and used on later reads and
  self-tests. It is validated against a character set, not a list, so bridge variants need no code.
- If the read fails, the drive stays visible with a notice naming the closed access gate: the
  container's device permissions, AppArmor, or a missing read of the drive. Silent omission is not an
  option, per the lsblk-parity rule.
- Hot-plug: no udev watching, because that would mean constant monitoring, so a newly plugged drive
  appears on the next scan, on demand or scheduled. This is the deliberate trade for zero idle cost.

## 6. Packaging and configuration

- **Image:** a Go build stage and an Alpine runtime with `smartmontools` and `util-linux` from apk
  (`ADR-020`). The Alpine tag is not pinned in prose because Dependabot bumps it. Alpine rather than a
  distroless base because both tools are packaged there, and `smartctl`'s vendor quirk database is the
  product's core (`ADR-002`).
- **Configuration:** everything through the UI, persisted in SQLite, plus environment variables for
  bootstrapping: `SMARTSERVE_ADDR` (`:8080`), `SMARTSERVE_DB`, and the optional `SMARTSERVE_AUTH_USER`
  with `SMARTSERVE_AUTH_PASSWORD` (or `_FILE`).
- **Data volume:** `/data` holds the SQLite file and its WAL siblings.

## 7. Security posture

- Device access is **read-only** except for self-tests: the only invocation that touches a drive's
  state is `smartctl -t short|long`, and those two values are its entire allow-list (`A19`).
- The UI is open by default. Optional single-user basic auth (`ADR-033`) is enabled by setting a
  username and password in the environment: no user table, no registration, sessions in memory only,
  so a restart signs everyone out. `docs/DEPLOYMENT.md` covers the reverse-proxy alternative.
- Container privilege is the operator's choice (least-privilege options in `docs/DEPLOYMENT.md`); any
  process that can read raw disk devices can read all data on those disks, and the docs say so.
