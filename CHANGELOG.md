# Changelog

Notable changes per release. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] — unreleased

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
- Notification secrets are stored in the SQLite database and therefore in the `/data` volume.
  Environment and `*_FILE` secret resolution is deferred (`ADR-021`); treat the volume as
  sensitive.

### Known limitations

- SMART self-tests are not yet triggerable from the UI (`FEATURES.md` A19).
- Error-log *entry* lists are not displayed — only their counts. No drive available to the
  project has ever logged an error, so the populated shape has never been observed
  (`FEATURES.md` A20).
- Native AHCI/SATA controller access is untested; SATA drives have only been exercised
  through a USB bridge.
- The idle-footprint figures are targets, not measurements, pending a run on a public
  instance.

[1.0.0]: https://github.com/Masieono/Smart-Serve/releases/tag/v1.0.0
