# Smart-Serve — Requirements Traceability & Open Questions

This document closes the design phase. Every requirement from the originating brief
is mapped to the design section that satisfies it, and every remaining open question
is listed for a decision before implementation begins.

## 1. Traceability matrix

| ID | Requirement (from the brief) | Where it is designed | Status |
|---|---|---|---|
| R1 | Docker container web application | `DESIGN.md` §6, `DEPLOYMENT.md` | Covered |
| R2 | S.M.A.R.T. for internal **and** USB-attached storage | `DESIGN.md` §5, `SMART_INTERPRETATION.md` §1 | Covered |
| R3 | "If it shows up on `lsblk`, it shows in the web interface" | `DESIGN.md` §3 Stage 1 (lsblk is source of truth) | Covered |
| R4 | "Scan" button offering an immediate view | `SCOPE.md` MVP#1, `API.md` `POST /api/scan` + SSE | Covered |
| R5 | Ability to scan on-demand | `SCOPE.md` MVP#1, `DESIGN.md` §3 | Covered |
| R6 | Schedule regular scans on the host machine | `SCOPE.md` MVP#5, `ADR-004`, `DATA_MODEL.md` `scan_schedules` | Covered |
| R7 | Easy-to-understand overall drive health (CrystalDiskInfo-class) | `SMART_INTERPRETATION.md` §2/§4 | Covered |
| R8 | Remaining longevity of scanned drives | `SMART_INTERPRETATION.md` §5 | Specified now; ships Phase 2 |
| R9 | ntfy and other notification integrations | `NOTIFICATIONS.md`; ntfy + webhook in MVP, Gotify/Telegram/Discord/SMTP in Phase 2 | Covered |
| R10 | Alerts when a failing drive is detected | `NOTIFICATIONS.md` `health_change` transition events | Covered |
| R11 | As lightweight / out-of-the-way as possible when idle | `DESIGN.md` §2.1 (enforced invariant), `ADR-001`, `ADR-007` | Covered |
| R12 | Save per-drive info (name the boot drive, per-drive notes) | `SCOPE.md` MVP#4, `DATA_MODEL.md` `device_notes`, `API.md` notes endpoint | Covered |

## 2. Decisions (resolved with the project owner)

| ID | Decision | Recorded in |
|---|---|---|
| D1 (was Q1) | **Missed-run policy is configurable** — env var `SMARTSERVE_MISSED_RUN_POLICY` (`skip` default \| `run_once`) *and* a Settings-UI toggle. `skip` waits for the next scheduled fire. | `ADR-004`, `ADR-009`, `DEPLOYMENT.md` |
| D2 (was Q2/Q3) | **All interpretation thresholds live in the Settings UI** (SQLite) with documented sensible defaults — *not* one env var per threshold. Env vars are reserved for bootstrap/operational settings only. | `ADR-009`, `DATA_MODEL.md`, `API.md` |
| D3 (was Q4) | `device_appeared` is **opt-in per channel** (default priority `low`). | `NOTIFICATIONS.md` |
| D4 (was Q5) | RAID-controller pass-through stays **out of scope**. | `SCOPE.md` non-goals |
| D5 (was Q6) | **No default schedule** on first run; the user creates schedules explicitly. | `SCOPE.md`, `DATA_MODEL.md` |
| D6 (was Q7) | **Authentication** — off by default. Optional **single-user basic auth** is specified and not built (`ADR-033`); multi-user accounts and roles remain out of scope, where a reverse proxy is the answer. | `ADR-016`, `ADR-033`, `DEPLOYMENT.md` |
| D7 (was Q8) | **180-day** snapshot retention default. | `DATA_MODEL.md` |
| D8 | **No hot-plug watcher** confirmed — on-demand/scheduled scans are the freshness mechanism. | `ADR-007` |
| D9 | **Go stack accepted** on the strength of the idle-cost analysis; implementation risks and mitigations are documented. | `ADR-001`, `ADR-010` |

## 3. Decisions from follow-up reviews (D10–D20)

| ID | Decision | Recorded in |
|---|---|---|
| D10 (was Q9) | **Self-test** — a UI trigger offering **short/long**; scheduled self-tests also supported. **Built**, opt-in only and never automatic, with a progress reading (a long test on a large drive runs for hours). Device plumbing was the gate, and it is validated. | `SCOPE.md` P2#4/#9, `API.md`, `FEATURES.md` A19, `ADR-017`, `ADR-024` |
| D11 (was Q10) | **Report export** — four formats (plain text, self-contained web page, spreadsheet, JSON) chosen from an Export dialog. A per-format enable list and a zip bundle are **not** implemented; the formats are all always offered. | `API.md`, `DATA_MODEL.md`, `FEATURES.md` A8 |
| D12 (was Q11) | **Alert acknowledgement + maintenance/silence windows** — both **built**. Maintenance windows are a `maintenance_windows` setting (no table, no endpoints); acknowledgement is scoped to the verdict it was granted for and lives as columns on `device_notes`, rather than the table `ADR-017` proposed. | `SCOPE.md` P2#10, `NOTIFICATIONS.md`, `ADR-017` |
| D13 (was Q12) | **Both** a 0–100 health score **and** Good/Caution/Bad. Basic score ships in MVP; the trend-aware refinement lands in Phase 2. | `SCOPE.md` MVP#14 + P2#8, `FEATURES.md` A17 |
| D14 (was Q13) | **Multi-arch image (amd64 + arm64)** in MVP. Clarified: this is the host CPU architecture the container runs on (x86-64 servers vs ARM NAS / Raspberry Pi / Apple Silicon), not a different deployment model. | `DEPLOYMENT.md`, `FEATURES.md` C8 |
| D15 (was C18) | **Licensing** — application source is **MIT**; the image carries smartmontools' GPLv2 notices + source pointer. | `ADR-012`, `README.md` |
| D16 (was C11) | **Name** — "Smart Serve"; binary/image/env prefix `smartserve` / `SMARTSERVE_`. | throughout |
| D17 (was C14) | **Secrets** — env vars / Compose `.env` / `*_FILE` / DB, precedence **env > file > DB**; UI shows the source. | `ADR-013`, `DEPLOYMENT.md` |
| D18 (was C15) | **Health score** — 0–100 formula defined with verdict-aligned bands and a mandatory penalty breakdown. | `SMART_INTERPRETATION.md` §8 |
| D19 | **Reverse proxy** — app is proxy-agnostic; `SMARTSERVE_BASE_PATH` for subpaths; SSE + CSRF handled. | `ADR-015`, `DEPLOYMENT.md` |
| D20 (was C7) | **UI/theme** — dedicated `docs/UI.md`; no-framework vanilla stack; light/dark/auto + custom-accent themes. | `ADR-014`, `UI.md` |
| D21 (was C8) | **Repository & registry** — private GitHub repo `masieono/smart-serve`; image `ghcr.io/masieono/smart-serve`. The GHCR package must be made **public** at v1, otherwise users must build from source. | `DEPLOYMENT.md`, `README.md` |
| D22 (was C16) | **CI = GitHub Actions** — tests, multi-arch `buildx`, GHCR push. | `DEPLOYMENT.md` |
| D23 (was C1) | **No user management** — no accounts, no roles, no sessions; that stays out of scope and a reverse proxy is the answer for it. Narrowed by `ADR-033`, which specifies an optional single user. CSRF still enforced (`ADR-015`). | `ADR-016`, `ADR-033`, `DEPLOYMENT.md` |
| D24 (was C17) | **Versioning** — semantic versioning + changelog; image tags `latest` and `x.y.z`. | `DEPLOYMENT.md` |
| D25 (was C19) | **Base path** — default `/`; `SMARTSERVE_BASE_PATH` for subpath hosting. | `ADR-015` |
| D26 | **The NAS fleet tier** — accepted after a review of the fleet use case: dense fleet table (`D8`), grouping by tag (`D9`), bay/slot field (`D10`), import/export of labels & notes promoted to P2 (`C7`), projection window follows retention (`A26`), "replace first" ranking (`A24`), fleet-wide export (`A23`), warranty/RMA evidence pack (`A27`), testing session (`D11`), and correlated-change detection (`A25`, P3). Batch self-test **tabled**. | `SCOPE.md` Phase 2.5, `FEATURES.md` §A23–A27 / C7 / D8–D11 / §H, `ADR-018` |
| D27 | **Working order** — documentation → implementation → UI spot-cleaning, for every batch. | `FEATURES.md` §I, `SCOPE.md` "UI consolidation passes", `ADR-019` |
| D28 | **Optional single-user auth** — HTTP Basic, one user, configured from the environment, **off unless both variables are set**. No accounts, no sessions, no migration; the Settings UI cannot enable it; `/healthz` stays exempt so the image never carries a secret. Multi-user and roles remain out of scope. **Specified, not built.** | `ADR-033`, `FEATURES.md` C5, `SCOPE.md` |

## 4. Considerations — status

Lower-stakes items, each with a sensible default already in the docs. Confirm the
open ones or change them as you read through.

| ID | Consideration | Design | Status |
|---|---|---|---|
| C1 | Authentication | Off by default; an optional single user is specified (`ADR-033`, not built) | **Resolved → D6, D23** |
| C2 | Default thresholds | 55 °C; Bad ≥10; Caution on any non-zero; all UI-editable | **Confirmed** |
| C3 | First-run schedules | None until the user creates one (D5) | **Confirmed** |
| C4 | Retention | 180 d snapshots / 30 d notification log (D7) | **Confirmed** |
| C5 | `device_appeared` alerts | Opt-in per channel, priority low (D3). *USB S.M.A.R.T./diagnostics are always available regardless.* | **Confirmed** |
| C6 | Schedule time zone | `TZ` env var **and** a UI Settings field | **Confirmed** |
| C7 | Frontend stack | Vanilla JS, no framework/CDN | **Resolved → D20** |
| C8 | Image registry / namespace | `ghcr.io/masieono/smart-serve` (user **Masieono** confirmed) | **Resolved → D21** |
| C9 | RAID pass-through | Out of scope (D4) | **Confirmed** |
| C10 | Scan concurrency | Sequential, not parallel | **Confirmed** |
| C11 | Project name | "Smart Serve" | **Resolved → D16** |
| C12 | Runtime defaults | Port `8080`, data volume `/data` | **Confirmed** |
| C13 | Knowledge-base upkeep | Ships with releases **+ opt-in in-app refresh** (`ADR-011`) | **Confirmed** |
| C14 | Secrets at rest | env / `.env` / `*_FILE` / DB, env precedence | **Resolved → D17** |
| C15 | Health-score formula | Defined, with penalty breakdown | **Resolved → D18** |
| C16 | Testing & CI | Fixture interpreter tests + `buildx`; **GitHub Actions** | **Resolved → D22** |
| C17 | Versioning & releases | Semver + changelog; tags `latest`, `x.y.z` | **Resolved → D24** |
| C18 | Licensing | App **MIT**; image carries GPLv2 notices | **Resolved → D15** |
| C19 | Base path | Default `/`; `SMARTSERVE_BASE_PATH` for subpaths (D19) | **Resolved → D25** |
| C20 | Browser support | Broad evergreen support (Chrome/Firefox/Safari/Edge) | **Confirmed** |
| C21 | First-run onboarding | Guided empty state prompting the first Scan | **Confirmed** |
| C22 | Branding | Simple inline SVG mark; no external assets. *(Awareness: "Smart Serve" is also a registered mark of Smart Serve Ontario for alcohol training — negligible risk for a personal GitHub project, noted only for completeness.)* | **Confirmed** |
| C23 | GHCR package visibility | Repo + image stay **private during development**; image made **public** at v1 so users can pull | **Confirmed** |
| C24 | Dev workflow / live preview | `docker-compose.dev.yml` + bind-mounted source for local iteration and preview | **Confirmed** |

## 5. Assumptions

- **A1** Host is Linux with a working `lsblk` and a kernel that `smartctl` can read
  device nodes from.
- **A2** The container is granted device read access per `DEPLOYMENT.md` (the operator
  chooses the privilege level).
- **A3** MVP is single-user / trusted-LAN; no multi-tenancy.
- **A4** Drives normally expose a serial number; the `__nomodel:` fallback covers the
  exceptions (`ADR-005`).
- **A5** Scheduled scans honor the container's `TZ` (also editable in the UI) for
  cron-style schedules.
- **A6** Authentication is delegated to a reverse proxy; the app ships no login and no
  user management (D23).

## 6. Definition of done — design phase

**Met, and the project has moved past it.** The design phase is closed:

- All design documents reviewed and merged.
- Decisions D1–D25 accepted and considerations C1–C24 confirmed by the project owner.
- MVP acceptance criteria agreed (`SCOPE.md`) and met — all fourteen MVP items shipped.
- `ADR-001` through `ADR-017` accepted.

The MVP was built in the `SCOPE.md` MVP order: project skeleton → SQLite migrations → scan
pipeline (`lsblk` + `smartctl` + interpreter) → JSON/SSE API → embedded frontend →
notifications → container image. It is tagged `v0.1.0`.

Decisions continue past the design phase: `D26`/`D27` and `ADR-018`/`ADR-019` were added
after a review of the NAS fleet use case, which is where the open work now lives
(`SCOPE.md` Phase 2.5).
