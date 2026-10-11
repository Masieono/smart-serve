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
| D6 (was Q7) | **Reverse-proxy auth** for MVP; built-in single-user auth is Phase 2. | `DEPLOYMENT.md`, `SCOPE.md` |
| D7 (was Q8) | **180-day** snapshot retention default. | `DATA_MODEL.md` |
| D8 | **No hot-plug watcher** confirmed — on-demand/scheduled scans are the freshness mechanism. | `ADR-007` |
| D9 | **Go stack accepted** on the strength of the idle-cost analysis; implementation risks and mitigations are documented. | `ADR-001`, `ADR-010` |

## 3. Decisions from follow-up reviews (D10–D20)

| ID | Decision | Recorded in |
|---|---|---|
| D10 (was Q9) | **Self-test** — a UI trigger offering **short/long**, defaulting to **short**; the default is changeable in Settings; scheduled self-tests also supported. Phase 2 (committed). | `SCOPE.md` P2#9, `API.md`, `FEATURES.md` A19 |
| D11 (was Q10) | **Report export** — formats selectable in Settings, **default plain text**; a bundled multi-format download (zip) is offered in Phase 2. | `API.md`, `DATA_MODEL.md`, `FEATURES.md` A8 |
| D12 (was Q11) | **Alert acknowledgement + maintenance/silence windows** — implemented regardless of tier; scheduled as committed Phase 2 work ("MVP vs Phase 2" is release order, not scope). | `SCOPE.md` P2#10, `NOTIFICATIONS.md` |
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
| D23 (was C1) | **Authentication is out of scope** — no login, no user management; rely on reverse-proxy auth (Nginx). CSRF still enforced (`ADR-015`). | `ADR-016`, `DEPLOYMENT.md` |
| D24 (was C17) | **Versioning** — semantic versioning + changelog; image tags `latest` and `x.y.z`. | `DEPLOYMENT.md` |
| D25 (was C19) | **Base path** — default `/`; `SMARTSERVE_BASE_PATH` for subpath hosting. | `ADR-015` |

## 4. Considerations — status

Lower-stakes items, each with a sensible default already in the docs. Confirm the
open ones or change them as you read through.

| ID | Consideration | Design | Status |
|---|---|---|---|
| C1 | Authentication | No built-in auth — reverse proxy only (D23) | **Resolved → D23** |
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

- All ten design documents reviewed and merged.
- Decisions D1–D14 accepted; considerations C1–C18 confirmed by the project owner.
- MVP acceptance criteria agreed (`SCOPE.md`).
- ADR-001 through ADR-011 accepted (`DECISIONS.md`).

Implementation then proceeds in the `SCOPE.md` MVP order: project skeleton →
SQLite migrations → scan pipeline (`lsblk` + `smartctl` + interpreter) → JSON/SSE
API → embedded frontend → notifications → container image.
