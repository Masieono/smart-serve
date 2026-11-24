# Smart-Serve: Scope & Status

Smart-Serve is a self-hosted web app that reads S.M.A.R.T. data from the drives a Linux host
reports and says, in plain language, how each one is doing. It ships as one static binary with
embedded assets and a SQLite database, and it is idle-quiet: no device is read unless a scan, a
self-test or an open page asks for it.

This page states what the app does and does not do. The MVP and phase tiers used to live here;
they are closed, and the per-feature tier table is now [`FEATURES.md`](FEATURES.md). The reasoning
behind the decisions is in [`DECISIONS.md`](DECISIONS.md).

## What the app does

**One interface, five pages.**

- **Fleet** (`/`): one table, one row per drive, ordered worst first, with a glance line above it
  (health counts that double as filters, and how old the last scan is). Search across name, bay,
  serial, model, tag, filesystem and mountpoint; scan on demand.
- **Device** (`/device/<serial>`): verdict and score with the reasoning, the partition map and
  per-partition usage, trends over time, the full attribute table (value/worst/threshold/raw),
  SMART self-tests with live progress and abort, and the drive's own error and self-test logs.
- **Schedules**: interval or cron, enable/disable, last and next run, and a `selftest` action that
  tests one drive at a time.
- **Notifications**: channels (ntfy, webhook, Gotify, Telegram, Discord, SMTP), per-channel event
  and group subscriptions, the delivery log, and maintenance windows.
- **Settings**: thresholds (global and per drive model), notifications, appearance (themes and
  palettes), and data (retention, import/export, knowledge base source).

**Scanning and reporting.** `lsblk` parity including USB-attached drives, live SSE progress, a
per-device transport override for stubborn bridges, raw `smartctl` output, and an export dialog
offering plain text, a self-contained page, CSV or JSON.

**Interpretation.** Good/Caution/Bad from the drive's own health status and per-attribute
thresholds, a 0–100 score with a penalty breakdown, a longevity estimate with its confidence, a
worsening badge, a "since the last scan" diff, and a bundled drive knowledge base matched on model
and firmware.

**Operations.** `/healthz` for Docker, Prometheus `/metrics` (off by default), `SMARTSERVE_BASE_PATH`
for subpath hosting, a multi-arch image (amd64/arm64), import/export of labels, settings and
schedules, and optional single-user sign-in: off by default, sessions for browsers with an idle
timeout, Basic for scripts, `/healthz` exempt.

## The UI is one design

There is no second interface, no `?ui=` parameter, no card grid and no separate dashboard. The card
grid, the dashboard as its own page, the "replace first" shortlist card and the `dashboard_view`
setting were retired when the table became the fleet (`ADR-032`). The ranking they showed survives
as the ordering of the weekly digest's drive list.

## Explicit non-goals (never planned)

- **Writing to drives** in any form: settings changes, secure erase, formatting. Self-tests are the
  single, opt-in exception.
- **RAID-controller S.M.A.R.T. pass-through** (`-d megaraid`, …). May be revisited; not committed.
- **Hot-plug detection** via udev. On-demand and scheduled scans are the freshness mechanism.
- **Host metrics** beyond storage (CPU, RAM, network). Scope creep away from the S.M.A.R.T. mission.
- **Multi-user accounts, roles and user management.** A reverse proxy is the answer (`D23`); the
  single-user case is the optional sign-in.

## Deferred, and not yet verified

Deferred by decision (`ADR-023`):

- **Localization** (`D7`). The cost is extracting every string and keeping each locale in step,
  which is only worth doing for a translator who will maintain the result.
- **Error-log entry lists** (`A20`). Counts and self-test entries are shown; a populated error entry
  has never been captured from real hardware.

Documented but unverified on hardware:

- **RAID/LVM/ZFS member naming.** The mapping is written from blkid's own tokens and has never been
  seen on a disk that carries one.
- **NVMe on ARM.** arm64 is verified end to end through USB SAT; NVMe has not been tested there.
- **Image size re-measurement.** Pending a release that changes the base image (`RELEASE.md`).

## Verification status

The core loop is verified on real hardware: native NVMe, USB-attached NVMe, USB-attached SATA, a
native SATA controller, and a USB flash drive with no S.M.A.R.T. at all, which is reported as such
rather than as a failure. Idle cost, sampled 2026-11-18 (`TESTING.md` 8.5): **18.9 MiB** on amd64 by
the cgroup, **19.5 MiB** on the Pi 5 as RSS and PSS (that host's cgroup cannot account memory, so PSS
is the figure the budget is stated against), 11 file descriptors and 11 threads flat, CPU near zero.
arm64 is verified in part (native execution, startup health, a full SMART read, self-test progress);
overnight stability and broader parity remain open.
