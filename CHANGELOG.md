# Changelog

Notable changes per release. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Two Phase 4 items: the drive's own logs are now shown, and a self-test can be started, watched and
stopped from the device page.

### Added

- **Fleet-wide export (`A23`)** — the whole fleet as one file, from two links on the dashboard: a
  **CSV** with one row per drive, which is what you sort when planning a purchase, and a
  **printable page**, which is what you attach when several drives are going back at once. Both
  render from the same column list and the same rows the dashboard shows, through the same ranking,
  so the export cannot disagree with the screen. Three rules: **an absent reading is an empty cell,
  never a zero** (a blank is what a spreadsheet's `AVERAGE` skips; a `0` drags every average down and
  reads as a measurement), **the export is never filtered** (not by the dashboard's filter, not by
  hidden — a wrong number gets noticed and a missing row does not), and **the rows are in the
  ranking's own order**, because that is the one thing a reader cannot recover from the file.
- **Warranty/RMA evidence pack (`A27`)** — every report format now says what it is for, and names
  the evidence: the attributes failing the drive's **own manufacturer threshold**, plus the counters
  this app reads when it assesses a drive, from the same list the verdict uses. The default text
  format had no attribute table at all, so a report could say `BAD` and name no reason a support
  engineer could check. Selecting by the drive's threshold rather than ours keeps the report from
  contradicting itself: it is a listing, not a verdict, so a threshold changed since the scan cannot
  make the two disagree. It claims evidence, not proof.
- **Config import/export (`C7`)** — one JSON bundle of device labels, settings and schedules, with
  Export and Import in Settings. For a host migration: names, bays and tags live in the database,
  which survives a restart but not a move, and re-labelling sixteen drives by hand is an evening's
  work. **Import merges**, with the file winning for what it contains — a restore that deleted
  anything it did not recognise would be one you could not safely run twice — except schedules,
  which are replaced and say so. **Channels are deliberately excluded**: their config holds tokens
  and passwords, an export file is exactly the thing that gets emailed around, and the app already
  refuses to serve secret values over the API. The bundle carries a note saying so, so a file found
  in a drawer a year later explains itself.
- **"Replace first" ranking (`A24`)** — the dashboard carries a shortlist ordered by how strongly
  each drive argues for replacement, answering "which one do I buy this month?" rather than "how
  does each drive look today". It uses **bands** (replace now / soon / watch) rather than a weighted
  score, so it can show its reasons — "failing now", "7 new sector problems in 30 days", "94% of its
  rated write endurance used" — because a reader can disagree with a band and act on it, and cannot
  do either with an unexplained number. Write endurance can outrank a healthy verdict, since it is a
  countdown rather than a state. The card is absent when nothing is worth replacing, and capped at
  five entries. A disconnected drive is left out: its stored verdict is history, not a reading.
- **Snapshot diff (`A18`)** — a "Since the last scan" card on the device page showing what moved
  between the two most recent scans: verdict, score, temperature, wear, bad sectors, and the
  counters that moved. The server decides which counter movements are findings, using the drive's
  *own* threshold and the lost-storage set the interpretation already sums, so the card cannot
  disagree with the verdict; everything else that moved is counted and named as a count rather than
  dropped. An absent reading renders as an em dash, never a zero, and "nothing changed" is stated in
  words rather than left as an empty panel.
- **Group by tag/pool (`D9`)** — the dashboard can group the list by tag, so a NAS reads as three
  pools instead of twenty-four drives. It is a toggle rather than a filter, offered only when
  something is actually tagged, and a drive with several tags appears under **each** of them —
  first-tag-wins would be tidier but would silently drop a drive from a group it belongs to. The
  header says outright that the counts can add up to more than the fleet size, and untagged drives
  come last. Grouping works in both the card and table views; the table uses a separator row that
  spans every column, so its header stays the only one.
- **Dense fleet table (`D8`)** — a Cards/Table switch on the dashboard. The table gives one row per
  drive with bay, verdict, score, transport, model, capacity, temperature, age and last scan, and
  its headers sort — clicking the sorted column reverses it. A card grid is right for four drives
  and wrong for twenty-four, so the grid stays for small hosts. The list endpoint now carries
  `power_on_hours` and `wear_pct` so a row can show a drive's age without opening it.
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

### Changed

- **The projection window follows retention (`A26`).** The longevity estimate, the worsening flag
  and the trend adjustment all looked at a fixed 180 days, so an owner who raised `retention_days`
  to five years precisely to watch a slow trend had that history discarded by the projection anyway.
  All three now use the retention setting itself, so what the user keeps is what the app reads.
  There is one number rather than two, and it is the same one the pruning uses.
- **The health score is now trend-aware (`A17`).** A verdict is a statement about *now*; a score is
  a summary, and a drive whose bad sectors are climbing is worse than one sitting at the same count
  and going nowhere. Such a drive now loses 3 points for its first new sector and one more per
  doubling, capped at 10, with the reason appended to the verdict rationale as a rate and a window
  rather than a date. The cap is what keeps it honest: the trend adjusts the summary, and is too
  small to overrule the drive's own data or carry it out of its verdict on its own. A drive that
  could not be read is not adjusted at all.
- **The trend card's x-axis is time, not reading index** (`ADR-023`). One change gives all three
  things the rework was for: a 30-day window now spans 30 days whether or not anything was recorded
  in them, so a drive that stopped being scanned leaves a *visible gap* rather than a line that
  quietly ends at the right edge; a series that began five days ago occupies its own fifth of the
  window instead of being stretched edge to edge; and a shared date axis under the card makes the
  span readable **without hovering**. The hover marker and the readout are positioned on the same
  time scale as the line, so pointing at a chart now gives the reading nearest the pointer rather
  than one a fraction of the way through the array. Per-metric y-scaling is unchanged.

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
