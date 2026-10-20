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
  attribute detail, temperature, a 0–100 health score, and per-metric trend
  sparklines (health score, temperature, power-on hours, wear) with a 7/30/90-day
  range selector.
- **Drive naming & notes** — name your drives ("boot", "backup pool") and save free-form
  notes per device. Identity is keyed by serial number, so notes survive reboots, replug
  events, and device-letter changes.
- **Export anywhere** — plain text (the format to attach to a warranty claim), a
  self-contained web page, a spreadsheet, or JSON. A report says what it is for and names
  the evidence a claim needs: the attributes failing the drive's **own** manufacturer
  threshold, and the counters the app reads. The whole fleet exports as one CSV to sort, or
  as one printable page.
- **Alert notifications** — get notified when a drive's health state changes. ntfy,
  Gotify, Telegram, Discord, email (SMTP) and generic webhooks; health-transition
  events with squelching so a failing drive doesn't spam you on every scan, and
  maintenance windows for planned work.
- **Tiny footprint** — single static Go binary, embedded frontend, SQLite storage in
  one file, and a small Alpine-based image: about **15 MB compressed**, 54 MB on disk
  (`ADR-020`).
- **Clean, themable UI** — a sleek, navigable interface designed as a first-class
  requirement: dark / light / auto, Catppuccin, Dracula, Nord, Gruvbox, Synthwave,
  Hacker, Solarized, Tokyo Night and One Dark palettes, plus a custom accent colour —
  and a theme picker that never touches component code (`docs/UI.md`).

## Quick start

```bash
docker run -d \
  --name smart-serve \
  -v /dev:/dev:ro \
  -v smartserve-data:/data \
  -p 8080:8080 \
  --device-cgroup-rule='a *:* rwm' \
  --cap-add SYS_ADMIN \
  --cap-add SYS_RAWIO \
  --restart unless-stopped \
  ghcr.io/masieono/smart-serve:latest
```

> The three device flags are **not optional**. Mounting `/dev` only makes the nodes
> *visible*; without the cgroup rule every `open()` returns `EPERM`, and without the two
> capabilities a native NVMe read and a USB-bridge read each fail separately. All three
> fail identically, which is why they are listed together — see
> [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md) for least-privilege options and USB specifics.

> **Smart-Serve ships no authentication, by design** (`ADR-016`). It expects a reverse
> proxy to provide it. Publishing port 8080 as above is appropriate on a trusted LAN and
> nowhere else.

Then open `http://<host>:8080` and press **Scan**.

## Documentation

| Document | Contents |
|---|---|
| [`docs/DESIGN.md`](docs/DESIGN.md) | Architecture, process model, scan pipeline, scheduler, resource budget |
| [`docs/SCOPE.md`](docs/SCOPE.md) | Feature tiers: MVP, Phase 2, Phase 3 backlog |
| [`docs/DATA_MODEL.md`](docs/DATA_MODEL.md) | SQLite schema, retention policy, migrations |
| [`docs/API.md`](docs/API.md) | Internal REST + SSE API specification |
| [`docs/SMART_INTERPRETATION.md`](docs/SMART_INTERPRETATION.md) | How SMART values become CrystalDiskInfo-style verdicts and longevity estimates |
| [`docs/NOTIFICATIONS.md`](docs/NOTIFICATIONS.md) | Event catalog, the six channel adapters, squelching and maintenance windows |
| [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md) | Docker options, device access, security notes, USB guidance |
| [`docs/DECISIONS.md`](docs/DECISIONS.md) | Architecture Decision Record (ADR) log |
| [`docs/REQUIREMENTS.md`](docs/REQUIREMENTS.md) | Requirements traceability, decisions, considerations, assumptions |
| [`docs/FEATURES.md`](docs/FEATURES.md) | Full feature catalog & suggestions (tiers, idle-cost impact) |
| [`docs/UI.md`](docs/UI.md) | UI/UX design: navigation, components, theming, screen specs, accessibility |
| [`docs/RELEASE.md`](docs/RELEASE.md) | Release checklist, screenshot list, image-size and arm64 verification notes |
| [`CHANGELOG.md`](CHANGELOG.md) | What changed in each release |

## Development

`make ci` runs exactly what CI runs, in the same order, so a green run locally means a
green run there:

```bash
make ci
```

That target is four steps and nothing else. On a machine without `make` — a server, a
container — run them directly; they are identical:

```bash
gofmt -l .                                 # prints anything unformatted; must be empty
go vet ./...
go test -count=1 ./...
node --test internal/webui/jstest/*.test.mjs
```

You need Go (the version in `go.mod`) and Node 24. Nothing else.

| Target | Does |
|---|---|
| `make ci` | the four checks above |
| `make build` | builds `bin/smart-serve`, stamped with `git describe` |
| `make docker` | builds the image locally as `smart-serve:dev` |
| `make test-js` | JavaScript tests only |

**These checks are not how you deploy.** Running the app is `docker compose up -d --build`
or the `docker run` above (`docs/DEPLOYMENT.md`). The checks are for changing the code, and
they run in CI on every push whether or not you run them yourself.

## Project status

**MVP complete.** The documents in `docs/` define the planning, scope, and design of
the application. All decisions are recorded in
[`docs/REQUIREMENTS.md`](docs/REQUIREMENTS.md) (decisions D1–D27; considerations
C1–C24 all confirmed or resolved) and [`docs/DECISIONS.md`](docs/DECISIONS.md)
(ADR-001–ADR-020). Image: `ghcr.io/masieono/smart-serve`. CI: GitHub Actions.

Every `SCOPE.md` MVP item is built, and the Phase 2 work delivered so far is
history/trending, remaining-longevity estimates with a degradation warning, the
notification adapters (Gotify, Telegram, Discord, SMTP), maintenance windows, and the
theme palettes. The remaining work is SMART self-tests — deliberately last, because a
self-test is the only feature that writes to a drive (`docs/FEATURES.md` A19).

## Implementation status

The core loop works end to end: discover, interpret, persist, display, schedule,
alert.

- [x] Go module + domain model (`internal/model`)
- [x] Interpretation engine: verdicts + 0–100 health score (`internal/interp`) — unit-tested
- [x] smartctl/lsblk wrappers, JSON parsing, device discovery (`internal/smart`) — unit-tested
- [x] Scan orchestration + `smart-serve scan` CLI (`internal/scan`, `cmd/smartserve`)
- [x] SQLite persistence + migrations (`internal/store`) — unit-tested
- [x] HTTP API + SSE (`internal/httpapi`, `internal/events`) — unit-tested
- [x] Web UI (`internal/webui`) — dashboard, device detail with trend sparklines,
      schedules, notifications, settings; light/dark/auto, Catppuccin, Dracula, Nord
      and Gruvbox palettes, plus a custom accent colour
- [x] Scheduler (`internal/sched`) — interval + cron, in-process, sleeps to the next
      boundary rather than ticking — unit-tested
- [x] Notifications (`internal/notify`) — ntfy, Gotify, Telegram, Discord, SMTP and
      webhooks; transition-based alerts, squelching, maintenance windows, delivery
      log, channel test — unit-tested
- [x] Multi-format report export (`txt` / `html` / `csv` / `json`), saying what it is for and
      naming the evidence for a warranty claim (`A27`) — unit-tested
- [x] Fleet-wide export as one CSV or one printable page (`A23`) — unit-tested
- [x] Dockerfile, docker-compose (prod + dev), GitHub Actions CI
- [x] Docker device-access validation on real hardware — a native NVMe, a USB-attached
      NVMe, and a USB-attached SATA SSD all read correctly; a drive with no S.M.A.R.T.
      is reported as such rather than as a failure (`docs/DEPLOYMENT.md`)

## License

The application source is licensed under the **MIT License** — see [`LICENSE`](LICENSE).

The published container image bundles **smartmontools** (`smartctl`), which is
licensed **GPLv2**; the image ships its license text and a pointer to its
corresponding source. The app invokes `smartctl` as a separate process, so the two
are aggregated, not linked (`docs/DECISIONS.md` ADR-012).
