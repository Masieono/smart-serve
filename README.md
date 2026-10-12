# Smart-Serve

A lightweight, self-contained Docker web application that reports S.M.A.R.T. health
information for **all** storage devices visible to the host — internal SATA/NVMe drives
*and* USB-attached drives. If it shows up in `lsblk`, it shows up in the web interface.

Smart-Serve is deliberately built to be **out of the way**: a single Go binary that idles
at ~0% CPU and ~10–15 MB RAM. It only wakes up when you look at it, when you press
**Scan**, or when a scheduled scan fires. No polling loops, no metrics scrapers, no
database daemon.

## Feature highlights

- **On-demand scanning** — a Scan button that immediately enumerates every connected
  device (`smartctl --scan-open` + `lsblk` cross-referenced) and pulls full S.M.A.R.T.
  data for each.
- **Scheduled scanning** — configure recurring S.M.A.R.T. scans (interval or cron-style)
  from the web UI. The scheduler lives inside the container; no host cron needed.
- **CrystalDiskInfo-style readouts** — Good / Caution / Bad verdicts per drive, per-
  attribute detail, temperature, and (Phase 2) remaining-longevity estimates for SSDs
  and HDDs based on wear metrics and degradation trends.
- **Drive naming & notes** — name your drives ("boot", "backup pool") and save free-form
  notes per device. Identity is keyed by serial number, so notes survive reboots, replug
  events, and device-letter changes.
- **Alert notifications** — get notified when a drive's health state changes (e.g.
  ntfy and generic webhooks at MVP). Health-transition events with squelching so a
  failing drive doesn't spam you on every scan.
- **Tiny footprint** — single static Go binary, embedded frontend, SQLite storage in
  one file, distroless+smartmontools image well under 20 MB.
- **Clean, themable UI** — a sleek, navigable interface designed as a first-class
  requirement: dark / light / auto plus custom accent-colour themes, and a theme
  picker that never touches component code (`docs/UI.md`).

## Quick start

```bash
docker run -d \
  --name smart-serve \
  -v /dev:/dev:ro \
  -v smartserve-data:/data \
  -p 8080:8080 \
  ghcr.io/masieono/smart-serve:latest
```

> Device access is required for S.M.A.R.T. queries. See
> [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md) for least-privilege options and USB
> specifics before running in production.

Then open `http://<host>:8080` and press **Scan**.

## Documentation

| Document | Contents |
|---|---|
| [`docs/DESIGN.md`](docs/DESIGN.md) | Architecture, process model, scan pipeline, scheduler, resource budget |
| [`docs/SCOPE.md`](docs/SCOPE.md) | Feature tiers: MVP, Phase 2, Phase 3 backlog |
| [`docs/DATA_MODEL.md`](docs/DATA_MODEL.md) | SQLite schema, retention policy, migrations |
| [`docs/API.md`](docs/API.md) | Internal REST + SSE API specification |
| [`docs/SMART_INTERPRETATION.md`](docs/SMART_INTERPRETATION.md) | How SMART values become CrystalDiskInfo-style verdicts and longevity estimates |
| [`docs/NOTIFICATIONS.md`](docs/NOTIFICATIONS.md) | Event catalog, ntfy/webhook adapters, squelching rules |
| [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md) | Docker options, device access, security notes, USB guidance |
| [`docs/DECISIONS.md`](docs/DECISIONS.md) | Architecture Decision Record (ADR) log |
| [`docs/REQUIREMENTS.md`](docs/REQUIREMENTS.md) | Requirements traceability, decisions, considerations, assumptions |
| [`docs/FEATURES.md`](docs/FEATURES.md) | Full feature catalog & suggestions (tiers, idle-cost impact) |
| [`docs/UI.md`](docs/UI.md) | UI/UX design: navigation, components, theming, screen specs, accessibility |

## Project status

**MVP complete.** The documents in `docs/` define the planning, scope, and design of
the application. All decisions are recorded in
[`docs/REQUIREMENTS.md`](docs/REQUIREMENTS.md) (decisions D1–D25; considerations
C1–C24 all confirmed or resolved) and [`docs/DECISIONS.md`](docs/DECISIONS.md)
(ADR-001–ADR-016). Repository: private under GitHub user **Masieono**, image
`ghcr.io/masieono/smart-serve` (made public at v1). CI: GitHub Actions.

Every `SCOPE.md` MVP item is built. The remaining work is Docker deployment
validation on real hardware and the Phase 2 features (history/trending, longevity
estimates, further notification adapters, SMART self-tests).

## Implementation status

The core loop works end to end: discover, interpret, persist, display, schedule,
alert.

- [x] Go module + domain model (`internal/model`)
- [x] Interpretation engine: verdicts + 0–100 health score (`internal/interp`) — unit-tested
- [x] smartctl/lsblk wrappers, JSON parsing, device discovery (`internal/smart`) — unit-tested
- [x] Scan orchestration + `smart-serve scan` CLI (`internal/scan`, `cmd/smartserve`)
- [x] SQLite persistence + migrations (`internal/store`) — unit-tested
- [x] HTTP API + SSE (`internal/httpapi`, `internal/events`) — unit-tested
- [x] Web UI (`internal/webui`) — dashboard, device detail, schedules, notifications,
      settings; dark/light/auto + custom-accent themes
- [x] Scheduler (`internal/sched`) — interval + cron, in-process, sleeps to the next
      boundary rather than ticking — unit-tested
- [x] Notifications (`internal/notify`) — ntfy + webhook, transition-based alerts,
      squelching, delivery log, channel test — unit-tested
- [x] Dockerfile, docker-compose (prod + dev), GitHub Actions CI
- [ ] Docker device-access validation on real hardware (`docs/DEPLOYMENT.md`)

## License

The application source is licensed under the **MIT License** — see [`LICENSE`](LICENSE).

The published container image bundles **smartmontools** (`smartctl`), which is
licensed **GPLv2**; the image ships its license text and a pointer to its
corresponding source. The app invokes `smartctl` as a separate process, so the two
are aggregated, not linked (`docs/DECISIONS.md` ADR-012).
