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

**Status:** all fourteen items above are implemented, and the one open question — Docker
device access — is settled. It was validated end-to-end on real hardware against a native
NVMe, a USB-attached NVMe, and a USB-attached SATA SSD, plus a USB flash drive that
reports no S.M.A.R.T. at all and is now reported as such rather than as a failure
(`DEPLOYMENT.md`).

**Native SATA is now verified.** A second host (Debian, `dende`) exposed a SATA SSD on a
native controller — `lsblk` reports `TRAN=sata` and `smartctl` reads it as `sat` — and it reads
cleanly through the same three gates the reference compose file opens. The earlier caveat, that
the validated SATA disk had only ever been reached through a USB bridge, no longer applies.

**Measured.** The two resource criteria above are now verified on real hardware. On a
long-running instance with no browser open, `docker stats` reports **0.00% CPU** and
**13.36 MiB** RSS — inside the "near zero" and "under 20 MB" the MVP was written against. See
`RELEASE.md` for how to re-measure.

**Documentation corrected for the public release.** Three claims did not match the code and
were fixed rather than carried into a release: the image is Alpine, not distroless
(`ADR-020`); notification secrets are database-only, because the environment and `*_FILE`
sources `ADR-013` described were never built (`ADR-021`); and the README quick start omitted
the device-access flags it needs.

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
   hardware-affecting feature. **Unblocked and next to be built:** Docker device access is
   validated on real hardware (`DEPLOYMENT.md`), which was the gate. It is opt-in and
   never automatic, and a running test must show a progress reading, because a long test
   on a large drive runs for hours (`FEATURES.md` A19, `ADR-017`).
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
   (`FEATURES.md` A19/A20). `A20` **done**: the counts, the entries, the drive's own self-test
   timings, and smartctl's message when a log cannot be read. The error log renders a table only
   when its entries say something beyond which entry they are — on a drive smartctl does not
   recognise they say nothing else, and the count says it better. `A19` is next, and remains the
   only feature that writes to a drive.
10. **Alert acknowledgement & maintenance/silence windows** (`FEATURES.md` B7/B8).
    Maintenance windows **done**; acknowledgement **deferred** (`ADR-017`).
11. **ntfy action buttons / priority / tags** and a per-channel event-subscription UI
    (`FEATURES.md` B10/B12). Subscription UI **done**; ntfy action buttons **not
    started**.

## Phase 2.5 — "the NAS fleet"

Added after a review of what a NAS owner actually needs, which the MVP's
workstation-with-four-drives sizing does not cover. Every item is a view over data the app
already stores, so none of them adds idle cost. Per-item rationale is in `FEATURES.md`;
the trade-offs are in `ADR-018`.

1. **Dense fleet table** (`D8`) — one sortable row per drive, ordered by risk, with the
   existing filter/sort work (`D5`) feeding it. The card grid stays for small hosts.
2. **Grouping by tag/pool** (`D9`) — the organising idea for a fleet, and the precondition
   for correlated-change detection.
3. **Bay/slot field** (`D10`) — the physical address of a drive, on the card and in the
   report. A serial identifies a drive; a bay *finds* it.
4. **Import/export labels, notes & config** (`C7`, promoted from Phase 3) — so names,
   notes and tags survive a host migration instead of being re-entered by hand.
5. **Projection window follows retention** (`A26`) — stop discarding the long history a
   fleet owner deliberately kept.
6. **"Replace first" ranking** (`A24`) — verdict + direction + remaining life, ordered,
   with the reasons shown.
7. **Fleet-wide export** (`A23`) and the **warranty/RMA evidence pack** (`A27`).
8. **Testing session** (`D11`) — the bench workflow for drives that are not part of the
   fleet.
9. **Correlated-change detection** (`A25`) — P3, and last here: it needs grouping and
   history first, and a high bar, so that it cannot fire on a legitimate RAID rebuild.

## Phase 4 — committed, in implementation order

Everything below is wanted. This is the order it lands in, recorded in `ADR-023` so a later session
resumes rather than re-decides. Each round is a shipping unit, and a UI consolidation pass follows
each (`ADR-019`).

1. **The drive's own data, finished.** `A20` entry rendering, then `A19` self-test: the opt-in
   button first, scheduling after, staggered **one drive at a time** so a fleet does not start
   twenty-four long tests together. `B9` (self-test result notifications) rides on `A19`.
2. **The fleet foundation.** `D5` filter & sort, `D8` dense fleet table, `D9` grouping by pool,
   `D10` bay/slot field.
3. **Insight and the trend display.** `A18` snapshot diff, `A17` trend-aware health score, and the
   **trend-section rework**: a time-based x-axis with a shared date axis, so a 30-day window spans
   30 days whether or not anything was recorded, and the span is readable without hovering.
4. **The rest of the fleet tier.** `C7` import/export, `A26` projection window follows retention,
   `A24` "replace first" ranking, `A23` fleet export, `A27` warranty evidence, `D11` testing
   session, and `A25` correlated-change detection with **configurable** thresholds.
5. **Knowledge base.** `A21`, seeded from the owner's Western Digital, Seagate and Samsung drives
   and grown by community pull requests.
6. **Notifications polish.** `B7` acknowledgement, `B10` ntfy action buttons, `B13` weekly digest,
   group-level routing.
7. **Interfaces and last mile.** `C6` Prometheus `/metrics` (off by default), `D6` side-by-side
   comparison, custom per-model attribute thresholds.

**Deferred, and the one item deliberately left out of the above:** `D7` localization. The expensive
part is not the translations but extracting every string and keeping every locale in step, and that
work is only worth doing for a translator who will maintain the result (`ADR-023`).


## Phase 3 — backlog (nice-to-have, no commitments)

- Multi-user accounts / roles. **Conflicts with `D23`** (authentication is delegated to a reverse
  proxy), so this needs that decision revisited rather than simply built.
- Anything not covered by Phase 4, added as it comes up.

## UI consolidation passes

Each feature batch is followed by a **UI spot-cleaning pass** rather than more features:
the interface is reviewed as a whole — grouping, hierarchy, what to demote into a
secondary surface — and the outcome recorded in `UI.md`. An interface that grows
feature-by-feature without one stops being sleek, which is the product's stated
character. See `FEATURES.md` §I and `ADR-019`.

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
