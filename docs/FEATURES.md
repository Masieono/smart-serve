# Smart-Serve — Feature Catalog & Suggestions

A comprehensive inventory of features for Smart-Serve, including proposals beyond the
original brief. Tiers follow `SCOPE.md`. Every feature must preserve the
**zero-idle-cost invariant** (`DESIGN.md` §2.1): nothing here may poll devices or run
timers while the app is idle.

**Legend** — tier: `MVP` / `P2` / `P3`; idle cost:
💤 adds no idle cost · 🔌 only active during a scan · ⚠ needs an opt-in guard to
avoid idle cost.

## A. Core reporting (the S.M.A.R.T. mission)

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| A1 | Device list with `lsblk` parity (internal + USB) | MVP | 🔌 | lsblk is source of truth |
| A2 | On-demand **Scan** button with live SSE progress | MVP | 🔌 | |
| A3 | Device detail: full attribute table (value/worst/threshold/raw) | MVP | 💤 | CrystalDiskInfo layout |
| A4 | Overall verdict Good/Caution/Bad with plain-language rationale | MVP | 🔌 | |
| A5 | Temperature, power-on hours, power cycles, capacity | MVP | 🔌 | |
| A6 | Firmware version, serial, WWN, interface speed display | MVP | 💤 | from smartctl |
| A7 | Human-readable drive age ("in service 4.2 years") | MVP | 💤 | derived from power-on hours |
| A8 | Copy / export S.M.A.R.T. report | MVP | 💤 | **done** — plain text, self-contained web page, `field,value` spreadsheet, or JSON, chosen from an Export dialog |
| A9 | Health summary bar (n good / n caution / n bad) | MVP | 💤 | **done** — folded into the Devices header as a compact clickable list beside the title and search box |
| A10 | Raw `smartctl` output viewer (diagnostics toggle) | MVP | 💤 | vital for USB bridge quirks |
| A11 | Per-device transport override (`-d sat`, …) | MVP | 💤 | **done** — offered on the device page for any drive whose S.M.A.R.T. cannot be read, and shown as currently set |
| A12 | Attribute help tooltips ("what is Reallocated Sectors?") | MVP | 💤 | educational, CDI-like |
| A13 | Scan history log (durations, per-device errors) | MVP | 💤 | |
| A14 | Degradation trend detection ("worsening" early-warning badge) | P2 | 💤 | **done** — see below |
| A15 | History charts (temp, sectors, wear) + sparklines | P2 | 💤 | **done** — per-metric sparkline card on the device page, with a 7/30/90-day/all range selector |
| A16 | Remaining-longevity estimate (NVMe / SSD / HDD) | P2 | 💤 | **done** — see below, with confidence labels |
| A17 | Health score 0–100 + letter grade | MVP/P2 | 💤 | basic in MVP; trend-aware in P2 |
| A18 | Snapshot diff ("what changed since last scan") | P2 | 💤 | |
| A19 | SMART self-test (manual short/long + scheduled) + self-test log | P2 | ⚠ | **eventually — opt-in only, see below** |
| A20 | SMART error log display | P2 | 🔌 | ATA error log / NVMe error log |
| A21 | Drive knowledge base (rated TBW, known firmware advisories) | P3 | 💤 | bundled static table; opt-in in-app refresh |
| A22 | Usable support for drives with no S.M.A.R.T. (USB flash drives, card readers) | P2 | 💤 | **done** — see below |

### A14/A16 — Longevity and the worsening badge

Both come from the same history, and both are deliberately modest, because a S.M.A.R.T.
reading is a snapshot of a device that does not know its own future.

**Longevity** is extrapolated from the drive's own wear counter (NVMe `percentage_used`),
reported as a horizon — *"80% of rated life left, about 320 days at the current rate"* —
with a confidence that reflects how long the drive was actually watched (`low` under a
month, `medium` beyond) and its caveat shown **inline**, not behind a tooltip: a caveat
nobody reads is not a caveat. A window shorter than a day produces no rate at all, because
two scans an hour apart say nothing about remaining life.

**Sector growth is a rate, never a date.** Counts arrive in bursts, and a date
extrapolated through two of them looks precise and means nothing — so the app reports
*"7 new sector problems in 60 days"* and says why there is no forecast. For the same
reason, ATA wear counters are left alone: they are vendor-specific enough that guessing at
one is worse than reporting none, so an ATA drive falls back to the sector basis.

**The worsening badge** (`A14`) fires when bad sectors are accumulating, on the dashboard
card and the device page. It is separate from the verdict badge because the two answer
different questions: the verdict says how the drive looks *now*, the badge says which way
it is going — and a drive can be green today and still be the one worth replacing first.

A drive with too little history says so rather than guessing, and a drive that reports no
sector counts is *absent* from the trend rather than reported as flat: no history is not
the same finding as a clean bill of health.

### A22 — Drives with no S.M.A.R.T.

Most USB flash drives and card readers do not implement S.M.A.R.T. at all, and some USB
bridges do not pass it through. `smartctl` fails on them, which used to mean the app
showed a raw error and nothing else. It cannot invent health data, so it does the two
things that are actually available:

- **Say so plainly.** The device page shows a neutral **notice**, not an error box: the
  drive does not report S.M.A.R.T., here is why, and the identity, capacity, filesystem
  and mountpoints shown come from the operating system rather than the drive. The
  dashboard card carries a *No S.M.A.R.T.* badge instead of *Unknown*, which would read
  as "not worked out yet", and no health ring is drawn because there is no score to draw.
- **Report the facts lsblk has.** `removable`, `fs_version` and lsblk's `REV` (used as
  the firmware revision when smartctl cannot supply one) are stored alongside the rest.
  For a flash drive these are most of what can be said about it, and they cost nothing —
  `lsblk` already ran.

The notice also carries the per-device `-d` **transport override** (`A11`), because a
real SSD or HDD inside a USB enclosure usually *does* have S.M.A.R.T. and just needs the
right bridge protocol.

Deliberately **not** done: filesystem-usage graphs, write/read benchmarks, or a synthetic
health score for a drive that reports nothing. Those would be inventing data the drive
never provided, which is exactly the failure mode this app exists to avoid.

### A19 — SMART self-test: how it should behave when it lands

Deferred, but the constraints are worth recording before anyone builds it.

- **Opt-in, and never automatic.** Nothing fires a self-test because the app started,
  because a scan ran, or because a drive appeared. The user presses a button, or
  creates a schedule they explicitly asked for. This is the only feature that writes
  to a drive.
- **A running test needs a progress reading.** A long test on a 14 TB drive runs for
  many hours — that is the normal case for the drives this app targets, not an edge
  case. A spinner with no percentage is indistinguishable from a hang, so the UI must
  surface the drive's own progress figure (the ATA self-test polling minutes, or the
  NVMe self-test log's percent-complete) and say so plainly when a drive reports
  none.
- **It lands last.** Docker device access has to be validated on real hardware first
  (`DEPLOYMENT.md`): a self-test is the one operation where getting the device
  plumbing wrong actually matters.

## B. Alerts & notification

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| B1 | ntfy channel (first-class) | MVP | 💤 | |
| B2 | Generic webhook channel | MVP | 💤 | |
| B3 | Health-transition alerts with squelching | MVP | 💤 | |
| B4 | Temperature alerts | MVP | 💤 | |
| B5 | Device appeared / disappeared alerts | MVP | 💤 | appeared is opt-in |
| B6 | Scheduled-scan failure alerts | MVP | 💤 | |
| B7 | Alert acknowledgement (mute a known issue, keep the verdict) | P2 | 💤 | **deferred, not dropped** — belongs in the app; the state it needs costs more than the noise it removes at single-host scale |
| B8 | Maintenance / silence windows (suppress during e.g. array rebuild) | P2 | 💤 | **done** — a `maintenance_windows` setting, one-off or recurring (daily/weekly), optionally scoped to drives picked from a list; evaluated at dispatch time, so no idle cost |
| B9 | Self-test result notifications (pass/fail) | P2 | 💤 | |
| B10 | ntfy action buttons ("Open dashboard"), priority, tags | P2 | 💤 | |
| B11 | Gotify / Telegram / Discord / SMTP adapters | P2 | 💤 | **done** — each a small `Sender`; the editor is data-driven, so adding another is a data change |
| B12 | Per-channel event subscription UI | P2 | 💤 | |
| B13 | Weekly digest (one summary instead of per-event) | P3 | 💤 | |

## C. Operations & deployment

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| C1 | Docker image + compose reference | MVP | 💤 | |
| C2 | `/healthz` endpoint for Docker `HEALTHCHECK` | MVP | 💤 | trivial, aids orchestrators |
| C3 | SQLite single-file storage + retention pruning | MVP | 💤 | |
| C4 | Reverse-proxy auth documented (nginx/Caddy) | MVP | 💤 | |
| C5 | ~~Built-in single-user basic auth~~ **Not planned** — auth delegated to a reverse proxy (D23) | — | 💤 | |
| C6 | Prometheus `/metrics` exporter | P3 | ⚠ | **off by default** — scraping invites polling |
| C7 | Import/export config & notes (JSON) | P3 | 💤 | migrate between hosts |
| C8 | Multi-arch image (amd64 / arm64) | MVP | 💤 | home NAS / Raspberry Pi |
| C9 | Configurable base path for subpath hosting | MVP | 💤 | `SMARTSERVE_BASE_PATH` (`ADR-015`) |

## D. UI / UX

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| D1 | Dashboard cards + device detail view | MVP | 💤 | |
| D2 | Theme picker: dark / light / auto / custom accent colour | MVP | 💤 | **done** — plus Catppuccin (4 flavours), Dracula, Nord, Gruvbox, Synthwave, Hacker, Solarized, Tokyo Night and One Dark palettes |
| D3 | Responsive mobile layout | MVP | 💤 | |
| D4 | Hide / unhide devices, and forget one entirely | MVP | 💤 | escape hatch for unreadable bridges; **forget** is for the plug-in-once workflow |
| D5 | Filter & sort devices (health, name, transport) | P2 | 💤 | |
| D6 | Side-by-side multi-device comparison | P3 | 💤 | |
| D7 | Localization (multi-language) | P3 | 💤 | CDI parity |

## E. Idle-cost guardrails (rules for accepting any new feature)

These rules exist so feature growth cannot erode the product's core promise:

1. **No periodic device reads** outside an explicit or scheduled scan.
2. **Time-based features sleep to the next event** — never tick on an interval.
   (Schedules, retention — computed as a single `time.Sleep`.) A feature that only
   needs to be consulted *when something else already happened* — maintenance
   windows, squelching — is evaluated at that moment instead, which is cheaper still.
3. **Any scraper-style endpoint** (Prometheus) ships **disabled by default**.
4. **UI freshness is always tied to the last scan** — never to live polling. The
   "last scanned" timestamp is visible on every screen.
5. **Anything that writes to a drive is opt-in and never automatic** — the user
   presses the button, or sets the schedule (A19).

## F. Explicitly rejected (see `SCOPE.md` non-goals)

- Write operations to drives (settings changes, secure erase, formatting).
- RAID-controller SMART pass-through (`-d megaraid`, …).
- udev / hot-plug watcher.
- Host CPU/RAM/network metrics — scope creep away from S.M.A.R.T.
- Filesystem-usage graphs — not S.M.A.R.T.; `lsblk` mountpoints are shown for
  context only.

## G. Drive knowledge base strategy (Phase 3)

The owner correctly flagged that a drive-spec knowledge base implies ongoing upkeep.
The strategy minimises that cost:

- **Base layer — smartmontools' upstream `drivedb.h`.** Already ships inside the
  `smartctl` binary in the image and is actively maintained upstream. It supplies
  model families, known-issue flags, and vendor attribute quirks **for free**, with
  no effort from this project.
- **Curated layer — a small bundled JSON table** (rated TBW, rated workload, release
  year, known firmware advisories) that `drivedb.h` does not carry. Community-
  contributed, versioned in-repo, schema-versioned so the app tolerates data both
  older and newer than itself.
- **Update mechanism — ships with each release.** No runtime network fetch, so the
  app stays offline-capable and adds zero idle cost. A future **opt-in** "refresh
  knowledge base" action (only when pressed, never polling) can address freshness
  without breaking the invariant.
- **Graceful degradation.** A drive absent from the knowledge base still gets the
  full S.M.A.R.T. report — only the extras (rated TBW, advisories) are missing. No
  device is ever hidden or downgraded for lacking KB data.
- **Cadence.** Best-effort quarterly-ish updates tied to releases; contributions via
  pull request.

