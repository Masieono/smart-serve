# Smart-Serve — Architecture & Design

## 1. Overview

Smart-Serve is a single Go binary packaged with `smartmontools` into a small Docker
image. It discovers storage devices, queries their S.M.A.R.T. data, interprets it into
human-friendly health verdicts, persists snapshots and user notes, and can notify
external services when drive health changes.

Design goals, in priority order:

1. **Coverage** — every device in `lsblk` appears in the UI, including USB drives.
2. **Idle frugality** — ~0% CPU and <20 MB RAM when not scanning or serving a page.
3. **Self-containment** — one container, one port, one data volume; zero host setup.
4. **Clarity** — CrystalDiskInfo-grade readable health reporting, not raw SMART dumps.

Non-goals (explicitly out of scope): writing to drives, triggering self-tests (until
Phase 2), RAID-controller SMART pass-through, multi-tenant auth (reverse-proxy pattern
in the interim).

## 2. Process model

One process, one binary, four internal components:

```
┌───────────────────────────────────────────────────────────┐
│                    smart-serve (Go)                        │
│                                                           │
│  HTTP server ──► handlers ──► scan executor ──► smartctl   │
│      │                        │       lsblk                │
│      │                        ▼                           │
│      │                   SQLite (/data/smartserve.db)      │
│      │                        ▲                            │
│      ├──► scheduler (sleeps until next due scan) ─────────┤
│      │                        │                            │
│      └──► SSE hub ◄──────────  │                            │
│                               ▼                            │
│                        notifier bus ──► ntfy / webhook      │
└───────────────────────────────────────────────────────────┘
```

- **HTTP server** (stdlib `net/http`): serves the embedded frontend and the JSON API.
  With no requests in flight it consumes no CPU.
- **Scan executor**: runs the discovery + SMART query pipeline (Section 3).
  Serializes scans behind a mutex — only one scan runs at a time; concurrent scan
  requests join the in-flight scan rather than launching another.
- **Scheduler**: a single goroutine that sleeps until the next due scheduled scan,
  then invokes the scan executor. Between scans it does nothing. Schedules are
  interval-based (`every N hours`) or cron-style (`daily at 03:00`) as defined in
  `docs/DATA_MODEL.md`.
- **Notifier bus**: publishes scan events to configured channels after each scan; no
  timers, no loops. SSE scan-progress events are pushed to connected browsers only.

### 2.1 Idle-resource budget (a design invariant)

| Condition | CPU | RAM |
|---|---|---|
| Idle, no browser connected | ~0% | ~8–12 MB (binary + SQLite page cache) |
| Idle, browser tab open | ~0% (SSE connection is epoll-driven) | +~1 MB per connection |
| Active scan (N devices) | one `smartctl` child at a time, sequential | +~5 MB per child process |

Enforcement rules: no background device polling of any kind; no per-second timers
anywhere in the code; the only goroutines that exist between events are the
scheduler's single `time.Sleep` and the HTTP accept loop. Device discovery happens
**only** inside an explicit or scheduled scan — page loads read from the SQLite
snapshot, never spawn `smartctl`.

## 3. Scan pipeline

A scan is a three-stage pipeline. All external commands are executed with a hard
timeout (default 15 s per invocation).

### Stage 1 — Discovery

1. `smartctl --scan-open --json` — returns devices smartmontools can open, including
   the transport hint needed for some USB bridges (e.g. `-d sat`, `-d usbjmicron`).
2. `lsblk --json --paths --output NAME,KNAME,MODEL,SERIAL,SIZE,TYPE,TRAN,MOUNTPOINTS,FSTYPE,ROTA,RM,WWN`
   — the authoritative list of what the *host* sees.

The **lsblk list is the source of truth** for UI coverage ("if it's in lsblk, it's in
the UI"). The lsblk tree is flattened: block devices of `TYPE == "disk"` are
candidates (loop devices and partitions excluded). Devices present in lsblk but
**absent** from smartctl's scan are still listed, marked `smart_available: false`
with the capture error (common for some USB bridge chips — see Section 5).

### Stage 2 — Per-device SMART query

For each discovered device, in sequence (never parallel — avoids I/O contention):

- ATA/SATA/USB: `smartctl -x --json <dev>` (with the transport hint from Stage 1
  when present; fallback chain in Section 5).
- NVMe: `smartctl -x --json /dev/nvme0` (controller) with per-namespace data as
  reported by smartctl.

`-x` (all) is preferred over `-a` because it includes vendor-specific and NVMe log
pages needed for wear estimation.

### Stage 3 — Interpret & persist

1. Normalize the raw smartctl JSON into an internal device record (ATA attribute
   table, NVMe health block, temperature, overall health string) — normalization
   rules live in `docs/SMART_INTERPRETATION.md`.
2. Compute the verdict (Good / Caution / Bad) per that document's algorithm.
3. Write a **snapshot** row (full normalized SMART data, JSON blob) plus upsert the
   device row; apply retention pruning.
4. Publish scan events to the notifier bus (health transitions, temperature
   breaches, device appearance/disappearance) per `docs/NOTIFICATIONS.md`.
5. Emit SSE progress events throughout (per-device started/finished, scan complete)
   so the UI's Scan button gives live feedback.

### 3.1 Single-flight behavior

The scan executor exposes `Scan(ctx) (ScanResult, error)` internally. A mutex +
condition variable means: pressing Scan while a scheduled scan is running joins it;
pressing Scan twice returns the same in-flight result; the UI shows one "scanning…"
state globally.

## 4. Frontend

Embedded via `embed.FS` — no CDN dependencies, no Node build tooling required.

- **Stack**: server-rendered HTML shell + vanilla JS (ES modules, no framework) +
  minimal hand-written CSS. Deliberately chosen over React/Vue to keep the binary
  small and the attack surface minimal. JS talks to the backend exclusively via the
  JSON API + one SSE connection (`GET /api/events`).
- **Views**:
  - **Dashboard**: device cards — verdict badge (Good/Caution/Bad color-coded),
    temperature, model, user-assigned name, mountpoint(s), last-scan timestamp —
    and the global **Scan** button.
  - **Device detail**: CrystalDiskInfo-style attribute table (ID, name,
    value/worst/threshold, interpreted raw value), temperature, capacity,
    power-on hours, power cycles, health verdict with rationale, and the
    edit form for name/notes/tags.
  - **Settings**: scan schedules, notification channels, scan options.
- **Rendering**: page loads never trigger scans. The dashboard renders the last
  snapshot from SQLite with a visible "last scanned: …" timestamp; freshness is a
  user action (the Scan button), matching the on-demand philosophy.

## 5. USB & bridge strategy

USB drives are a first-class concern (the original motivation for this project):

- `smartctl --scan-open` already tries the common SAT (SCSI-ATA-Translation) path,
  which covers most modern USB-SATA/USB-NVMe bridges.
- For devices where the default open fails, Smart-Serve retries a **fallback chain**
  of `-d` transports: `sat` → `sat,12` → `scsi` → `usbjmicron` → `usbcypress`.
  Only invoked for devices whose first capture failed; the first working transport
  is remembered per device (in `device_notes`-like internal state) and reused first
  next time to avoid repeated probing.
- If the entire chain fails, the device remains visible in the UI as
  `SMART unavailable` with the error message — never silently dropped, per the
  "lsblk parity" rule. A per-device "hide" flag (user metadata) is the escape hatch.
- Device hot-plug: Smart-Serve does **not** watch udev (that would require constant
  monitoring). A hot-plugged drive appears on the next scan — on-demand or
  scheduled. This is the intended trade for zero idle cost; the docs call it out
  honestly.

## 6. Packaging & configuration

- **Image**: multi-stage build — Go build stage → distroless/static base +
  statically-compiled `smartctl` binary copied in. Target: <20 MB compressed.
- **Config**: everything through the web UI (persisted in SQLite) plus a small set
  of environment variables for bootstrapping (`SMARTSERVE_ADDR` default
  `:8080`, `SMARTSERVE_DB` default `/data/smartserve.db`).
- **Data volume**: `/data` holds only the SQLite file (and its WAL siblings).

## 7. Security posture

- The app performs **read-only** raw-device access via `smartctl`. It exposes no
  endpoint that writes to or configures a drive. No `smartctl` invocation in the
  codebase may use write-capable commands (e.g. `-s`, offline tests) — enforced by
  an allow-list of arguments, not a deny-list.
- The web UI is unauthenticated at MVP by design (LAN tool, like most Docker
  monitoring apps). `docs/DEPLOYMENT.md` documents the reverse-proxy + basic-auth
  interim pattern and a Phase 2 built-in auth option.
- Container privilege is the user's choice (least-privilege options in
  `docs/DEPLOYMENT.md`); the docs are explicit that any process able to read raw
  disk devices can read all data on those disks.

