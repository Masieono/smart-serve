# Smart-Serve — Scope & Roadmap

Features are tiered to keep the MVP honest: a small, fully-working core first, then
layered capabilities. Each tier is a shipping unit; nothing in a tier depends on a
later tier. The authoritative per-feature tier table — including suggested additions
and their idle-cost impact — is [`FEATURES.md`](FEATURES.md).

## MVP (Phase 1) — "the core loop"

USB-attached drives are a **first-class target**, not an add-on: S.M.A.R.T. and
diagnostic support for plugged-in USB devices is part of the core scope (R2).

The application is useful and shippable with only these:

1. **On-demand scan button** — one click, live SSE progress, immediate dashboard
   refresh with every connected device (`lsblk` parity, including USB).
2. **Device dashboard** — cards for every device: verdict badge
   (Good/Caution/Bad), temperature, model, capacity, user-assigned name,
   mountpoint(s), filesystem, transport, last-scan timestamp.
3. **Device detail view** — CrystalDiskInfo-style table: all SMART attributes with
   ID, name, value/worst/threshold, interpreted raw values; overall health string;
   power-on hours, power-cycle count; the verdict rationale in plain language.
4. **Drive naming & notes** — per-device name, free-form notes, hide flag; persisted
   by serial number (survives reboot/replug/device-letter changes).
5. **Scheduled scanning** — schedule CRUD in the UI: interval (`every 6h`) or
   cron-style (`0 3 * * *`); enable/disable; last-run/next-run display. Scheduler
   runs inside the container.
6. **Health verdicts** — Good/Caution/Bad per `docs/SMART_INTERPRETATION.md`:
   smartctl health status + CrystalDiskInfo-equivalent thresholds on reallocated
   sectors (5), pending sectors (197), uncorrectable sectors (198), and temperature.
7. **Notifications: ntfy + generic webhook** — health-state transitions (with
   squelching), temperature above threshold, device disappeared/reappeared, scan
   failures. Channel config UI. See `docs/NOTIFICATIONS.md`. (Since extended with
   Gotify, Telegram, Discord and SMTP — see Phase 2 item 3.)
8. **Docker deployment** — published image, docker-compose reference, documented
   device-access options (`docs/DEPLOYMENT.md`).
9. **Storage engine** — SQLite persistence of devices, notes, snapshots, schedules,
   notification config (`docs/DATA_MODEL.md`), retention pruning.

10. **Diagnostics** — raw `smartctl` output viewer per device, and a per-device
    transport override (`-d sat`, …) for stubborn USB bridges (`FEATURES.md` A10/A11).
11. **Report export** — copy/download a CrystalDiskInfo-style S.M.A.R.T. report per
  device (`FEATURES.md` A8). Delivered as an Export dialog offering plain text, a
  self-contained web page, a spreadsheet, or JSON.
12. **At-a-glance extras** — health summary bar (now part of the Devices header),
  human-readable drive age, and firmware version / serial / WWN display
  (`FEATURES.md` A6/A7/A9).
13. **Polish & ops** — themes (dark/light/auto, the community palettes and a custom
  accent), responsive layout, hide-device escape hatch, `/healthz` for Docker
  `HEALTHCHECK`, multi-arch (amd64/arm64) image
  (`FEATURES.md` C2/C8, D2/D3/D4).

14. **Health score (basic)** — a 0–100 score + grade computed from current
    attributes, shown alongside Good/Caution/Bad (`FEATURES.md` A17).

**MVP acceptance criteria** (used to judge "done"):
- A host with one NVMe, one SATA, and one USB drive shows all three in the UI after
  one scan.
- Idle container CPU measures ~0% over a 10-minute window; RSS stays under 20 MB
  without an open browser.
- Unplugging the USB drive and pressing Scan updates the UI and (if scheduled scan
  runs later) fires one `device_disappeared` notification.
- Notes attached to the USB drive reattach to it after replug with a changed device
  letter.

**Status:** all fourteen items above are implemented. The one MVP item not yet
validated end-to-end is Docker device access on real hardware (`DEPLOYMENT.md`).

## Phase 2 — "insight"

Status is recorded per item, so the roadmap reflects what is actually shipped rather
than what was planned.

1. **History & trending** — **done.** Per-device snapshot charts (temperature, wear,
  power-on hours, health score) in a trend card on the device page, with a
  7 / 30 / 90 day / all range selector. Sector-count charts wait on the longevity
  work below.
2. **Remaining-longevity estimates** — **done**, with one deliberate departure from the
  plan. Wear is projected from the drive's own counter (NVMe `percentage_used`) as a
  horizon whose confidence reflects how long the drive was actually watched, with its
  caveat shown inline. Sector growth is reported as a **rate over the window, not as a
  date**: sector counts arrive in bursts, and a date extrapolated through two of them
  would look precise and mean nothing. ATA wear counters are left alone for the same
  reason — they are vendor-specific enough that guessing at one is worse than reporting
  none — so an ATA drive falls back to the sector trend. Computed in
  `internal/interp/longevity.go`, exposed as `projection` on the device detail endpoint.
3. **Notification adapters** — Gotify, Telegram, Discord, SMTP email. **Done.**
4. **SMART self-test triggering** (offline/short/extended) — the first
   hardware-affecting feature. **Deferred, and deliberately last:** it is opt-in and
   never automatic, and a running test must show a progress reading, because a long
   test on a large drive runs for hours (`FEATURES.md` A19, `ADR-017`).
5. **Squelch configuration** — user-tunable dedupe window. **Done** — the
   `squelch_seconds` setting.
6. **In-app knowledge-base refresh** — an opt-in, user-pressed action to update the
   drive knowledge base without waiting for a release (`ADR-011`). **Not started.**

7. **Degradation trend detection** — **done.** A *Worsening* badge on the dashboard card
   and the device page fires when bad sectors are accumulating over the recent window
   (`FEATURES.md` A14/A15), and the charts and sparklines (`A15`) are done. The badge is
   deliberately about direction rather than verdict: a drive can be green today and still
   be the one worth replacing first.
8. **Trend-aware health score** — refines the MVP score using history
   (`FEATURES.md` A17). **Not started.**
9. **Self-test scheduling + self-test log**, and **SMART error log** display
   (`FEATURES.md` A19/A20). **Not started.**
10. **Alert acknowledgement & maintenance/silence windows** (`FEATURES.md` B7/B8).
    Maintenance windows **done**; acknowledgement **deferred** (`ADR-017`).
11. **ntfy action buttons / priority / tags** and a per-channel event-subscription UI
    (`FEATURES.md` B10/B12). Subscription UI **done**; ntfy action buttons **not
    started**.

## Phase 3 — backlog (nice-to-have, no commitments)

- Multi-user accounts / roles.
- Prometheus exporter endpoint (`/metrics`) — opt-in, since it invites scraping
  (tension with the idle-cost philosophy; would be disabled by default).
- Device groups/tags with group-level notification routing.
- Side-by-side multi-device comparison view.
- Import/export of notes & settings (JSON) for migration between hosts.
- Custom attribute thresholds per device/model.
- Localization (CrystalDiskInfo parity includes non-English communities).
- Drive knowledge base — rated TBW, known firmware advisories (`FEATURES.md` A21).
- Weekly digest notifications — one scheduled summary instead of per-event
  (`FEATURES.md` B13).

## Explicit non-goals (never planned)

- **Writing to drives** in any form (settings changes, secure erase, formatting).
- RAID-controller SMART pass-through (`-d megaraid`, etc.) — may be revisited, but
  not committed.
- Real-time/hot-plug detection via udev/udev-monitor — conflicts with the
  zero-idle-cost invariant; scans (on-demand or scheduled) are the freshness
  mechanism.
- Host metrics beyond storage (CPU/RAM monitoring) — scope creep away from the
  S.M.A.R.T. mission.
- Built-in authentication / user management — delegate to a reverse proxy (D23).
