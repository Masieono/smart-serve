# Smart-Serve: Requirements Traceability & Decisions

Every requirement from the originating brief is mapped to what satisfies it, and every decision
taken during design is recorded with where it lives. All entries are settled; the status column
reflects the code, not the plan.

## 1. Traceability matrix

| ID | Requirement (from the brief) | Where it is satisfied | Status |
|---|---|---|---|
| R1 | Docker container web application | `DESIGN.md` §6, `DEPLOYMENT.md` | Built |
| R2 | S.M.A.R.T. for internal **and** USB-attached storage | `DESIGN.md` §5, `SMART_INTERPRETATION.md` §1 | Built |
| R3 | "If it shows up on `lsblk`, it shows in the web interface" | `DESIGN.md` §3 Stage 1 (lsblk is source of truth) | Built |
| R4 | "Scan" button offering an immediate view | `FEATURES.md` A2, `API.md` `POST /api/scan` + SSE | Built |
| R5 | Ability to scan on-demand | `FEATURES.md` A2, `DESIGN.md` §3 | Built |
| R6 | Schedule regular scans on the host machine | `ADR-004`, `DATA_MODEL.md` `scan_schedules`, Schedules page | Built |
| R7 | Easy-to-understand overall drive health (CrystalDiskInfo-class) | `SMART_INTERPRETATION.md` §2/§4 | Built |
| R8 | Remaining longevity of scanned drives | `SMART_INTERPRETATION.md` §5, `FEATURES.md` A16 | Built |
| R9 | ntfy and other notification integrations | `NOTIFICATIONS.md`; all six channel types | Built |
| R10 | Alerts when a failing drive is detected | `NOTIFICATIONS.md` `health_change` events | Built |
| R11 | As lightweight / out-of-the-way as possible when idle | `DESIGN.md` §2.1, `ADR-001`, `ADR-007` | Built and measured |
| R12 | Save per-drive info (name the boot drive, per-drive notes) | `DATA_MODEL.md` `device_notes`, `API.md` notes endpoint | Built |

## 2. Decisions (resolved with the project owner)

| ID | Decision | Recorded in |
|---|---|---|
| D1 (was Q1) | **Missed-run policy is configurable**: `SMARTSERVE_MISSED_RUN_POLICY` (`skip` default \| `run_once`) and a Settings toggle. `skip` waits for the next scheduled fire. | `ADR-004`, `ADR-009`, `DEPLOYMENT.md` |
| D2 (was Q2/Q3) | **All interpretation thresholds live in the Settings UI** (SQLite) with documented defaults, not one env var per threshold. Env vars are for bootstrap and operational settings only. | `ADR-009`, `DATA_MODEL.md`, `API.md` |
| D3 (was Q4) | `device_appeared` is **opt-in per channel** (default priority `low`). | `NOTIFICATIONS.md` |
| D4 (was Q5) | RAID-controller pass-through stays **out of scope**. | `SCOPE.md` non-goals |
| D5 (was Q6) | **No default schedule** on first run; the user creates schedules explicitly. | `DATA_MODEL.md` |
| D6 (was Q7) | **Authentication is off by default.** Optional single-user auth is built (`ADR-033`); multi-user accounts and roles stay out of scope, where a reverse proxy is the answer. | `ADR-016`, `ADR-033`, `DEPLOYMENT.md` |
| D7 (was Q8) | **180-day** snapshot retention default. | `DATA_MODEL.md` |
| D8 | **No hot-plug watcher**: on-demand and scheduled scans are the freshness mechanism. | `ADR-007` |
| D9 | **Go stack accepted** on the strength of the idle-cost analysis; implementation risks and mitigations are documented. | `ADR-001`, `ADR-010` |

## 3. Decisions from follow-up reviews (D10–D28)

| ID | Decision | Recorded in |
|---|---|---|
| D10 (was Q9) | **Self-test**: short and long, opt-in and never automatic, with live progress, abort, and scheduled runs. Built and validated on real hardware. | `FEATURES.md` A19, `API.md`, `ADR-024` |
| D11 (was Q10) | **Report export**: plain text, self-contained web page, spreadsheet, JSON, all offered from one Export dialog. A per-format enable list and a zip bundle are **not** implemented. | `API.md`, `FEATURES.md` A8 |
| D12 (was Q11) | **Alert acknowledgement and maintenance windows**, both built. Windows are a `maintenance_windows` setting; acknowledgement is scoped to the verdict it was granted for and lives as columns on `device_notes`. | `NOTIFICATIONS.md`, `ADR-017` |
| D13 (was Q12) | **Both** a 0–100 health score **and** Good/Caution/Bad, with a trend-aware adjustment for drives whose bad sectors are climbing. | `FEATURES.md` A17 |
| D14 (was Q13) | **Multi-arch image (amd64 + arm64)**: the host CPU the container runs on (x86-64 servers vs ARM NAS, Pi or Apple Silicon), not a different deployment model. | `DEPLOYMENT.md`, `FEATURES.md` C8 |
| D15 (was C18) | **Licensing**: application source is MIT; the image carries smartmontools' GPLv2 notices and a source pointer. | `ADR-012`, `README.md` |
| D16 (was C11) | **Name**: "Smart Serve"; binary, image and env prefix `smartserve` / `SMARTSERVE_`. | throughout |
| D17 (was C14) | **Secrets**: environment, Compose `.env`, `*_FILE` or database, precedence environment > file > database, per channel. The UI shows the source, never the value. | `ADR-013`, `ADR-022`, `DEPLOYMENT.md` |
| D18 (was C15) | **Health score**: 0–100 formula with verdict-aligned bands and a mandatory penalty breakdown. | `SMART_INTERPRETATION.md` §8 |
| D19 | **Reverse proxy**: the app is proxy-agnostic; `SMARTSERVE_BASE_PATH` for subpaths; SSE and CSRF handled. | `ADR-015`, `DEPLOYMENT.md` |
| D20 (was C7) | **UI and theme**: `docs/UI.md`; no-framework vanilla stack; light, dark, auto and custom-accent themes plus palettes. | `ADR-014`, `UI.md` |
| D21 (was C8) | **Repository and registry**: private GitHub repo `masieono/smart-serve`; image `ghcr.io/masieono/smart-serve`. The GHCR package must be made **public** at v1. | `DEPLOYMENT.md`, `README.md` |
| D22 (was C16) | **CI is GitHub Actions**: tests, multi-arch `buildx`, GHCR push. | `DEPLOYMENT.md` |
| D23 (was C1) | **No user management**: no accounts, no roles. Narrowed by `ADR-033` and `D28` to one optional operator; CSRF is still enforced (`ADR-015`). | `ADR-016`, `ADR-033`, `DEPLOYMENT.md` |
| D24 (was C17) | **Versioning**: semantic versioning plus changelog; image tags `latest` and `x.y.z`. | `DEPLOYMENT.md` |
| D25 (was C19) | **Base path**: default `/`; `SMARTSERVE_BASE_PATH` for subpath hosting. | `ADR-015` |
| D26 | **The NAS fleet tier** is built: fleet table, tags, bay/slot field, import/export of labels, projection window follows retention, replace-first ranking, a fleet-wide export endpoint (no button yet), warranty evidence in every export, bench drives excluded from the ranking by tag, and correlated-change detection. Batch self-test is tabled. The replace-first **card** is retired; the ranking orders the digest. | `FEATURES.md` §A23–A27 / C7 / D8–D11, `ADR-018`, `ADR-032` |
| D27 | **Working order**: documentation, then implementation, then UI spot-cleaning, for every batch. | `FEATURES.md` §I, `ADR-019` |
| D28 | **Optional single-user auth**: one operator from two environment variables, **off unless both are set**. Browsers get a session cookie with an idle timeout and a sign-out; scripts keep HTTP Basic; `/healthz` is exempt. No accounts, roles or migration; the Settings UI cannot enable it. Built. | `ADR-033`, `FEATURES.md` C5 |

## 4. Settled defaults (the design-review considerations)

- Thresholds: 55 °C; Bad at 10 or more reallocated, pending or uncorrectable sectors; Caution on any
  non-zero; all editable, now also per drive model (C2, `FEATURES.md` A3). No schedule until the
  user creates one (C3).
- Retention: 180 days of snapshots, 30 days of notification log (C4).
- `device_appeared` is opt-in per channel at low priority (C5). Schedules honour `TZ`, also editable
  in Settings (C6). Scans run sequentially (C10). Port `8080`, data volume `/data` (C12).
- Frontend is vanilla JS with no framework or CDN (C7, D20). Branding is one inline SVG mark (C22).
  Browser support is current evergreen Chrome, Firefox, Safari and Edge (C20), and first run shows an
  empty state prompting the first scan (C21). Local development uses `docker-compose.dev.yml` with
  bind-mounted source (C24).
- The knowledge base ships with releases, with an opt-in refresh (C13, `ADR-011`).

## 5. Assumptions

- **A1** Host is Linux with a working `lsblk` and a kernel `smartctl` can read device nodes from.
- **A2** The container is granted device read access per `DEPLOYMENT.md`; the operator chooses the
  privilege level.
- **A3** Single-user, trusted-LAN deployment; no multi-tenancy.
- **A4** Drives normally expose a serial number; the `__nomodel:` fallback covers the exceptions
  (`ADR-005`).
- **A5** Scheduled scans honour the container's `TZ`, also editable in the UI, for cron schedules.
- **A6** Authentication is either the optional single user (`D28`) or delegated to a reverse proxy.
  The app ships no accounts and no roles (`D23`).

## 6. Status

The design phase is closed and the build has moved past it.

- All design documents reviewed and merged; `ADR-001` through `ADR-033` accepted.
- Decisions D1–D28 are settled, and considerations C1–C24 resolved into §4.
- The MVP acceptance criteria were met, and every roadmap item is built. What remains is the short
  deferred list in `SCOPE.md` (localization, error-log entry lists, array-member naming, NVMe on ARM).

The MVP landed in its planned order: skeleton, migrations, scan pipeline (`lsblk`, `smartctl`,
interpreter), JSON/SSE API, embedded frontend, notifications, container image. It is tagged `v0.1.0`.
