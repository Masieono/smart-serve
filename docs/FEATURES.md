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
| A23 | Fleet-wide export (every drive in one CSV or page) | P2 | 💤 | NAS: what you paste into a spreadsheet when planning a purchase |
| A24 | "Replace first" ranking (verdict + direction + remaining life, ordered, with reasons) | P2 | 💤 | the dashboard sorts by *today's* severity; this answers "which one do I buy this month?" |
| A25 | Correlated-change detection (N drives in a group moving together → one alert about the enclosure) | P3 | 💤 | needs grouping + history first, and a high bar — a RAID rebuild legitimately heats everything |
| A26 | Projection window follows retention (or becomes a setting) | P2 | 💤 | today it is fixed at 180 d, so a NAS that keeps 5 years discards the history it deliberately kept |
| A27 | Warranty / RMA evidence pack (the exported report says what it is for) | P2 | 💤 | cheap — the export already exists (`A8`) |

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

### A23–A27 / C7 / D8–D11 — The NAS fleet

The MVP is sized for a workstation with a handful of drives. A NAS changes the *shape*
of the problem, and the reason each item exists is worth recording rather than leaving
as a bare list entry.

- **The dashboard has to scale.** A card grid is right for four drives and wrong for
  twenty-four; what a fleet owner wants is one list ordered by risk (`D8`), which the
  existing filter/sort work (`D5`) feeds.
- **Grouping is the organising idea** (`D9`). Tag drives by pool — `tank`, `backup`,
  `media` — and the fleet reads as three groups instead of twenty-four cards. Grouping
  is also the precondition for `A25`.
- **A drive needs a physical address** (`D10`). A serial number identifies a drive; a
  bay number *finds* it. Cheapest NAS feature, largest payoff at the moment something
  actually fails.
- **Labels must outlive the container** (`C7`, promoted from P3). Names, notes and tags
  live in the SQLite volume, so they survive a restart but not a host migration. For a
  fleet, re-labelling sixteen drives is the difference between a five-minute move and an
  evening's work.
- **Correlated change is the fleet-only insight** (`A25`). Eight drives in one enclosure
  share a temperature: a dying fan presents as *all eight* running hot, which per-drive
  monitoring reports as eight unrelated alerts — precisely the noise that teaches people
  to ignore alerts. One alert naming the group and the shared delta points at the
  enclosure instead of the drives. It is deliberately P3: it needs grouping and history
  first, and it needs a high bar, because a RAID rebuild legitimately heats everything.
- **The projection window is too short for a NAS** (`A26`). Retention is configurable;
  the projection window is fixed at 180 days. An owner who raises retention to five years
  is doing so precisely to watch a slow trend — and the app then discards it. The tension
  was flagged when `A16` was built; a NAS is the case that resolves it.
- **"Replace first" is the question a fleet asks** (`A24`). The verdict says how a drive
  looks *now*; the ranking says which one to buy a replacement for this month, combining
  verdict, direction (`A14`) and remaining life (`A16`), with the reasoning shown.
- **Bench work is the second use case** (`D11`). Testing a batch of drives you do not want
  in your permanent fleet: label them, test them, export the reports, forget them.
- **Evidence is cheap** (`A27`). The report export already exists; saying on it that it is
  warranty/RMA evidence costs almost nothing and is the whole reason some people run a
  scan at all.

None of these adds idle cost. They are views over data the app already stores, plus one
setting (`A26`).

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
- **It lands last.** Docker device access had to be validated on real hardware first
  (`DEPLOYMENT.md`), because a self-test is the one operation where getting the device
  plumbing wrong actually matters. **That is now done** — native NVMe, USB-attached NVMe
  and USB-attached SATA SSD all read correctly — so this item is unblocked and waiting
  only on itself.

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
| C7 | Import/export config, notes & labels (JSON) | P2 | 💤 | **promoted from P3** — for a NAS this is not a nice-to-have: it is the difference between restoring a file and re-labelling 16 drives |
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
| D8 | Dense fleet table view (one row per drive, sortable by risk) | P2 | 💤 | the dashboard is a card grid; at 24 drives that is a lot of scrolling |
| D9 | Device grouping by tag/pool, dashboard grouped | P2 | 💤 | turns 24 cards into 3 readable groups; also the precondition for `A25` |
| D10 | Bay/slot field (physical location on the card and in the report) | P2 | 💤 | at 3am you need the bay number, not the serial |
| D11 | Testing session (label, test and report a batch of bench drives without polluting the fleet) | P3 | 💤 | `D4`'s *forget* already covers the single-drive version of this |

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

## H. Tabled (recorded, not planned)

Kept here so a good idea is neither silently dropped nor accidentally rebuilt. Nothing in
this section has a tier or a date.

| # | Feature | Why it is tabled |
|---|---|---|
| H1 | Batch self-test ("test all N drives overnight") | `A19` gives the primitive; the batch form is a thin layer on top and is not worth designing before the primitive exists. Revisit once `A19` ships. |

## I. Sequencing: documentation → implementation → UI polish

Feature work lands in three moves, and the third is not optional:

1. **Documentation.** An idea is written into this catalog (and `SCOPE.md`, plus an ADR
   when it carries a real trade-off) *before* it is built, so it is recorded rather than
   rediscovered.
2. **Implementation**, with the test discipline the rest of the project already carries.
3. **UI spot-cleaning.** Every tier of features adds controls, and an interface that grows
   feature-by-feature without a consolidation pass stops being sleek. After each batch the
   UI is reviewed as a whole — grouping, hierarchy, what to demote into a secondary
   surface — and the result recorded in `UI.md`.

This is a working agreement rather than a phase: it repeats for every batch
(`ADR-019`).

