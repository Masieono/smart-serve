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
| A8 | Copy / export S.M.A.R.T. report | MVP | 💤 | formats selectable in Settings (default text); bundled multi-format download in P2 |
| A9 | Health summary bar (n good / n caution / n bad) | MVP | 💤 | at-a-glance |
| A10 | Raw `smartctl` output viewer (diagnostics toggle) | MVP | 💤 | vital for USB bridge quirks |
| A11 | Per-device transport override (`-d sat`, …) | MVP | 💤 | manual fix for stubborn bridges |
| A12 | Attribute help tooltips ("what is Reallocated Sectors?") | MVP | 💤 | educational, CDI-like |
| A13 | Scan history log (durations, per-device errors) | MVP | 💤 | |
| A14 | Degradation trend detection ("worsening" early-warning badge) | P2 | 💤 | `SMART_INTERPRETATION.md` §5 |
| A15 | History charts (temp, sectors, wear) + sparklines | P2 | 💤 | from snapshot store |
| A16 | Remaining-longevity estimate (NVMe / SSD / HDD) | P2 | 💤 | with confidence labels |
| A17 | Health score 0–100 + letter grade | MVP/P2 | 💤 | basic in MVP; trend-aware in P2 |
| A18 | Snapshot diff ("what changed since last scan") | P2 | 💤 | |
| A19 | SMART self-test (manual short/long + scheduled) + self-test log | P2 | ⚠ | first hardware-affecting feature; default short, configurable |
| A20 | SMART error log display | P2 | 🔌 | ATA error log / NVMe error log |
| A21 | Drive knowledge base (rated TBW, known firmware advisories) | P3 | 💤 | bundled static table; opt-in in-app refresh |

## B. Alerts & notification

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| B1 | ntfy channel (first-class) | MVP | 💤 | |
| B2 | Generic webhook channel | MVP | 💤 | |
| B3 | Health-transition alerts with squelching | MVP | 💤 | |
| B4 | Temperature alerts | MVP | 💤 | |
| B5 | Device appeared / disappeared alerts | MVP | 💤 | appeared is opt-in |
| B6 | Scheduled-scan failure alerts | MVP | 💤 | |
| B7 | Alert acknowledgement (mute a known issue, keep the verdict) | P2 | 💤 | avoids alert fatigue |
| B8 | Maintenance / silence windows (suppress during e.g. array rebuild) | P2 | 💤 | |
| B9 | Self-test result notifications (pass/fail) | P2 | 💤 | |
| B10 | ntfy action buttons ("Open dashboard"), priority, tags | P2 | 💤 | |
| B11 | Gotify / Telegram / Discord / SMTP adapters | P2 | 💤 | |
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
| D2 | Theme picker: dark / light / auto / custom accent colour | MVP | 💤 | token-based (`UI.md` §5) |
| D3 | Responsive mobile layout | MVP | 💤 | |
| D4 | Hide / unhide devices | MVP | 💤 | escape hatch for unreadable bridges |
| D5 | Filter & sort devices (health, name, transport) | P2 | 💤 | |
| D6 | Side-by-side multi-device comparison | P3 | 💤 | |
| D7 | Localization (multi-language) | P3 | 💤 | CDI parity |

## E. Idle-cost guardrails (rules for accepting any new feature)

These rules exist so feature growth cannot erode the product's core promise:

1. **No periodic device reads** outside an explicit or scheduled scan.
2. **Time-based features sleep to the next event** — never tick on an interval.
   (Schedules, silence windows, retention — all computed as a single `time.Sleep`.)
3. **Any scraper-style endpoint** (Prometheus) ships **disabled by default**.
4. **UI freshness is always tied to the last scan** — never to live polling. The
   "last scanned" timestamp is visible on every screen.

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

