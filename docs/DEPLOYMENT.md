# Smart-Serve — Deployment Guide

## Requirements

- Docker (or any OCI runtime: Podman, containerd/K8s).
- `smartmontools` ships **inside the image** — nothing to install on the host.
- **Multi-arch image**: `linux/amd64` and `linux/arm64` — x86-64 servers, ARM NAS
  boxes, Raspberry Pi, and Apple Silicon all pull the same tag (`ADR-010`, D14).
- Host kernel with device nodes visible to the container (options below).

## The device-access problem (read this first)

S.M.A.R.T. queries require read access to raw device nodes (`/dev/sda`,
`/dev/nvme0`, …). A plain container sees none of them. Options, most to least
privileged:

### Option A — full `/dev` mount (simplest, common for home labs)

```yaml
services:
  smart-serve:
    image: ghcr.io/masieono/smart-serve:latest
    ports:
      - "8080:8080"
    volumes:
      - smartserve-data:/data
      - /dev:/dev:ro            # read-only mount of host /dev
    restart: unless-stopped

volumes:
  smartserve-data:
```

Read-only `/dev` is sufficient for `smartctl` reads. Caveat: device nodes appear and
disappear with hotplug (a node opened mid-scan after removal can error — handled per
scan, retried next scan).

### Option B — enumerated `--device` passthrough (least privilege)

```bash
docker run -d \
  -p 8080:8080 \
  -v smartserve-data:/data \
  --device=/dev/sda --device=/dev/sdb --device=/dev/nvme0 \
  ghcr.io/masieono/smart-serve:latest
```

Most secure, but device letters shift across reboots and USB plugs — only practical
for stable internal disks. Not recommended for the USB use case.

### Option C — `--privileged` (works, but unnecessary)

The app never writes to devices, so the extra capabilities buy nothing over
Option A. Listed for completeness; discouraged.

> **Security note:** any process with read access to raw disks can read the entire
> contents of those disks. Smart-Serve performs only `smartctl` reads via a
> fixed argument allow-list (see `DESIGN.md` §7), but the *privilege* is
> root-equivalent regardless. Treat the container like a privileged host tool;
> do not expose port 8080 to the public internet without auth (below).

## USB drives

- Option A covers USB automatically: plugged drives appear under `/dev` and are
  picked up by the next scan (press **Scan**, or wait for the next scheduled scan —
  there is no hot-plug watcher, by design).
- Enclosures with non-SAT bridges may need the `-d` transport fallback chain —
  automatic, nothing to configure (`DESIGN.md` §5).
- **USB and hotplug with Option A:** device nodes for USB drives are created at
  plug time inside `/dev`, which is a live mount — so they are visible. However,
  if the container started before the drive was plugged, some setups still require
  the node to exist; a fresh scan re-runs `lsblk`/`smartctl --scan-open`, which
  re-resolves. If a drive stubbornly shows `smart_available: false`, check
  `dmesg` on the host and re-plug; document the exact error in the UI card.

## Authentication (interim pattern)

The app ships **no authentication or user management** by design (D23). If exposed
beyond localhost/LAN, front it with a reverse proxy:

```nginx
location /smartserve/ {
    proxy_pass http://127.0.0.1:8080/;
    auth_basic "Smart-Serve";
    auth_basic_user_file /etc/nginx/.htpasswd;
    proxy_http_version 1.1;
    proxy_set_header Connection "";   # keep SSE alive
    proxy_buffering off;              # SSE must not be buffered
}
```

SSE requires `proxy_buffering off` and HTTP/1.1 — noted here because it is the
most common reverse-proxy misconfiguration. Built-in basic auth is a Phase 2
feature (`SCOPE.md`).

## Data & backup

- All state lives in one SQLite file: `/data/smartserve.db` (+ `-wal`/`-shm`).
- **The database contains secrets** — ntfy tokens and webhook auth headers — so treat
  `/data` as sensitive (restrictive permissions; note that backups include them).
- Backup: stop the container, copy the file, restart. Or `sqlite3` `.backup` live.
- Restoring is dropping the file back and starting the container (migrations are
  idempotent, schema_version-checked).

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `SMARTSERVE_ADDR` | `:8080` | HTTP listen address (env-only) |
| `SMARTSERVE_DB` | `/data/smartserve.db` | SQLite path (env-only) |
| `TZ` | host default | Time zone for cron-style schedules (also editable in the UI) |
| `SMARTSERVE_MISSED_RUN_POLICY` | `skip` | `skip` \| `run_once` when a scheduled scan was missed while the container was down |
| `SMARTSERVE_COMMAND_TIMEOUT` | `15` | per-command timeout (s) |
| `SMARTSERVE_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `SMARTSERVE_BASE_PATH` | `/` | URL subpath when hosted behind a proxy at e.g. `/smartserve/` (`ADR-015`) |

**Configuration precedence (ADR-009):** env vars provide *defaults* only while the
setting is absent from the database. Once a setting is changed in the web UI it is
stored in SQLite and the DB value wins — the UI shows each setting's effective
source. All **interpretation thresholds** (temperature, sector-count thresholds,
retention, squelch windows) are configured **in the UI**, never via env vars.
`SMARTSERVE_ADDR` and `SMARTSERVE_DB` are env-only and never stored.

### Secrets

Notification secrets (ntfy tokens, webhook auth headers) can be supplied three ways,
with precedence **environment > mounted file > database** (`ADR-013`):

1. **Env vars**, including a Compose `env_file:` / `.env` file — note that **Docker
   Compose** reads `.env` and injects the variables; the container/app does not parse
   `.env` itself.
2. **`*_FILE` variables** pointing at mounted secret files, e.g.
   `SMARTSERVE_NTFY_TOKEN_FILE=/run/secrets/ntfy_token` (Docker-secrets convention).
3. **Stored in the database** via the web UI (convenient; no restart needed).

The UI shows each secret's effective source. Prefer (1) or (2) when you already
manage config as code; use (3) if you just want to paste a token into the UI.

### Licensing

The application source is **MIT**. The image bundles **smartmontools** (`smartctl`),
which is **GPLv2**; the image ships smartmontools' license text and a pointer to its
corresponding source. The app calls `smartctl` as a separate process, so the two are
aggregated, not linked (`ADR-012`).

## Verifying the deployment

1. `docker logs smart-serve` — startup should log schema version and scheduler
   state, then go quiet.
2. Open the UI → press **Scan** → every `lsblk` disk appears.
3. `docker stats smart-serve` — after the scan settles, CPU ~0%, memory <20 MB
   (without an open tab). This is the acceptance test for the idle-frugality
   invariant.

## Local development & live preview

So the app can be developed and previewed outside a deployment host:

- **`docker-compose.dev.yml`** builds the image from the working tree and bind-mounts
  the source, so a rebuild picks up changes without publishing.
- **Go**: run `go run ./cmd/smartserve` directly on a Linux dev box for the fastest
  loop (device access still requires permissions on `/dev`). Frontend changes need
  only a browser refresh — the assets are static and embedded at build time in
  release builds, served from disk in dev mode.
- **CI (GitHub Actions)**: on push — `go vet`, unit tests (interpreter fixtures),
  then a multi-arch `buildx` build; on tag — push to `ghcr.io/masieono/smart-serve`.
- **Preview**: the dashboard is reachable at `http://localhost:8080` from the dev
  compose stack; a private-repo image is not pullable by others until the GHCR
  package is made public (D21).
