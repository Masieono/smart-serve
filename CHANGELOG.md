# Changelog

Notable changes per release. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Two Phase 4 items: the drive's own logs are now shown, and a self-test can be started, watched and
stopped from the device page.

### Added

- **Filter & sort (`D5`)** — the device list can be ordered by risk, name, bay, model, capacity,
  temperature or last scan, and filtered by transport. Each sort field carries its own direction,
  because "first" means different things per field, and a drive that reports no reading sorts last
  rather than first. The transport filter offers only the transports actually present, and search
  now covers the bay as well as the name, model, serial and mountpoints.
- **Bay / slot field (`D10`)** — a drive can be given a physical location ("bay 4", "front top"),
  which then leads on its card and appears in every export. A serial identifies a drive; a bay
  *finds* it, which is the question someone standing in front of a machine at 3am actually has.
  It is free text, and unset bays are omitted from the card entirely rather than rendered as an
  empty row — a gap in the bookkeeping is not a finding about the drive.
- **Self-test result notifications (`B9`)** — when a self-test finishes, the result is sent to any
  channel subscribed to `self_test_result`, at high priority if it failed. It is the one event that
  reports good news, and it is not opt-in the way the scan heartbeat is: it only ever exists because
  somebody started a test, so it can never arrive unsolicited.
- **Self-test triggering (`A19`)** — the device page offers a short and a long test, shows the
  drive's own progress reading while one runs, and can stop it. This is the first and only control
  in the app that changes a drive, so it is opt-in by construction: nothing starts a test on its
  own, the test kind is an allow-list of two strings, and both actions sit behind a confirmation.
  Progress is the drive's own percentage remaining rather than a figure derived from a clock, and a
  drive that cannot have its self-test log read is told so instead of being offered a button that
  would only fail. Scheduling is not part of this — a fleet-wide schedule needs
  one-drive-at-a-time staggering first.
- **Drive logs (`A20`)** — the error log and the self-test log are rendered, with the drive's own
  self-test time estimates and smartctl's words when a log cannot be read.

### Changed

- The self-test card is the one place the UI polls a device: every 15 seconds, while a test it
  started is running and the card is still on screen. `FEATURES.md` §E records this as the single
  documented exception to "UI freshness is tied to the last scan", with the reasoning, and the poll
  stops on both ends — when the drive reports no test running, and when the card leaves the page.

## [1.0.1] — 2026-10-18

Four fixes found by using the first release in anger, plus the documentation corrections that
came out of deploying it.

### Fixed

- **The attribute `?` dialog rendered the literal word "null"** on every field with no "how to
  read it" line. The native `Element.append` stringifies a `null` argument into a text node, and
  one call site had bypassed the project's own helper that filters them out.
- **Changing the trend window threw the page back to the top.** The re-render mounted a one-line
  skeleton first, which collapsed the page height, so the browser clamped the scroll position to
  zero.
- **Rows smartctl cannot name** read `Unknown_Attribute`, which looks like a read failure. They
  are labelled "Vendor specific" now, with a note under the table explaining that smartctl does
  not recognise the drive model and the counters are vendor-private.
- **The drive-logs panel** said the entries were "in the raw output" without saying where. It
  names the sections: `ata_smart_error_log`, `ata_smart_self_test_log`,
  `nvme_error_information_log`.

### Changed

- Em dashes removed from 31 user-facing strings, every attribute tooltip included.
- The Alpine base tag is no longer named in prose. Dependabot bumps it, and a version written
  into documentation goes stale on the next bump.
- Dependencies: `alpine` 3.20 → 3.24, `docker/metadata-action` v5 → v6.

### Documentation

- **The idle footprint is measured, not a target**: 0.00% CPU and 13.36 MiB RSS on a
  long-running instance with no browser open, inside the "near zero" and "under 20 MB" the MVP
  was written against.
- **Native AHCI/SATA is verified.** A second host exposes a SATA SSD on a native controller,
  closing the caveat that SATA had only ever been reached through a USB bridge.
- **Image size published**: 14.8 MB content, 53.5 MB on disk. The withdrawn "well under 20 MB"
  claim was true of the compressed figure and wrong about everything else.
- A drive with a populated error log was finally captured, and it justifies the counts-only
  decision in the drive-logs panel: on a drive smartctl does not recognise, a logged error
  carries an error number and an index and nothing else.

## [1.0.0] — 2026-10-17

First public release.

### Added

- **Scanning** — on-demand and scheduled, over every block device the host exposes,
  including USB-attached drives. Live progress over SSE, interval or cron schedules, and a
  scheduler that lives inside the container with no host cron.
- **Health reporting** — CrystalDiskInfo-style Good / Caution / Bad verdicts with a
  plain-language rationale, a 0–100 score with a penalty breakdown, and the full attribute
  table with a `?` on every interpreted attribute explaining what it means and how to read
  it. A row the drive *itself* reports at or below its own threshold is marked.
- **History and trending** — per-metric sparklines over 7 / 30 / 90 days or all time, a
  remaining-longevity estimate projected from the drive's own wear counter with its
  confidence and caveat shown inline, and a *Worsening* badge that fires when bad sectors are
  accumulating.
- **Drive logs** — how many entries the error log and self-test log hold, and the drive's own
  estimate of how long its self-tests run.
- **Identity that survives hardware churn** — names, tags and notes are keyed by serial
  number, so they follow a drive across reboots, replugs and device-letter changes.
- **Diagnostics** — a raw `smartctl` output viewer per drive, and a per-device `-d` transport
  override for USB bridges that need one.
- **Notifications** — ntfy, Gotify, Telegram, Discord, SMTP email and generic webhooks, with
  health-transition events, squelching, maintenance windows, and per-channel event
  subscriptions.
- **Per-channel environment overrides** — any channel field can be supplied from the
  environment or a mounted file (`SMARTSERVE_CHANNEL_<LABEL>_<FIELD>`, with a `_FILE` form), so
  a notification token can stay out of the data volume and out of its backups. The editor shows
  the variable names and badges the fields the deployment supplies.
- **Export** — plain text (the format to attach to a warranty claim), a self-contained web
  page, a spreadsheet, or JSON.
- **Interface** — dark, light and auto themes, a custom accent, eleven community palettes,
  a responsive layout, and a health summary that doubles as a filter.
- **Deployment** — multi-arch image (amd64 / arm64), `/healthz` for Docker `HEALTHCHECK`,
  and `SMARTSERVE_BASE_PATH` for subpath hosting behind a reverse proxy.

### Security

- **No authentication is built in, by design** (`ADR-016`). Smart-Serve expects a reverse
  proxy to provide it. Do not expose it directly to an untrusted network.
- The container needs read access to raw device nodes, which requires `CAP_SYS_ADMIN` and
  `CAP_SYS_RAWIO`. That is a real privilege grant — read `docs/DEPLOYMENT.md` before
  deploying, and prefer the least-privilege option that works for your host.
- Notification channel credentials can be supplied from the environment or a mounted file
  (`SMARTSERVE_CHANNEL_<LABEL>_<FIELD>`, with a `_FILE` form), so a token need not live in the
  `/data` volume or in whatever backs it up. A `_FILE` that cannot be read fails the send and
  records why, rather than falling back to a stored value (`ADR-022`).

### Known limitations

- SMART self-tests are not yet triggerable from the UI (`FEATURES.md` A19).
- Error-log *entry* lists are not displayed — only their counts. No drive available to the
  project has ever logged an error, so the populated shape has never been observed
  (`FEATURES.md` A20).
- Native AHCI/SATA controller access is untested; SATA drives have only been exercised
  through a USB bridge.
- The idle-footprint figures are targets, not measurements, pending a run on a public
  instance.

[Unreleased]: https://github.com/masieono/smart-serve/compare/v1.0.1...HEAD
[1.0.1]: https://github.com/masieono/smart-serve/releases/tag/v1.0.1
[1.0.0]: https://github.com/masieono/smart-serve/releases/tag/v1.0.0
