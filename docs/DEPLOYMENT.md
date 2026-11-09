# Smart-Serve — Deployment Guide

## Requirements

- Docker (or any OCI runtime: Podman, containerd/K8s).
- `smartmontools` ships **inside the image** — nothing to install on the host.
- **Multi-arch image**: `linux/amd64` and `linux/arm64` — x86-64 servers, ARM NAS
  boxes, Raspberry Pi, and Apple Silicon all pull the same tag (`ADR-010`, D14).
  ARM hardware verification is **partial**: Pi 5 / DietPi execution, a full container SMART read
  through USB SAT, and app identity/attribute checks are confirmed for the tested disk. See
  [the dated test record](TESTING.md#pass-8--arm64-raspberry-pi) for coverage.
- **Image size**: about **15 MB compressed** to pull and **54 MB on disk** once extracted
  (`docker image ls` reports both, as *content size* and *disk usage*). The bundled
  `smartmontools` and `util-linux` are most of it (`ADR-020`).
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
    # All three are required, and the /dev mount above provides none of them.
    device_cgroup_rules:
      - 'a *:* rwm'             # 1. permission to open the nodes
    cap_add:
      - SYS_ADMIN               # 2. native NVMe reads
      - SYS_RAWIO               # 3. USB-bridge passthrough
    restart: unless-stopped

volumes:
  smartserve-data:
```

**Mounting `/dev` is necessary but not sufficient.** There are three independent gates
between the container and a S.M.A.R.T. read, and a bind mount opens none of them. All
three produce the same `EPERM`, which is why this is worth reading before experimenting.

**1. The device cgroup — the one that bites first.** Docker's default allow-list is
`null`, `zero`, `full`, `tty`, `random`, `urandom` and the ptys; every other device is
denied. So with `/dev` mounted the container can *see* every node and `open()` returns
`EPERM` on all of them:

```
Smartctl open device: /dev/nvme0n1 failed: Operation not permitted
```

The tell is that it fails on **every** device — SATA and USB alike — and that it fails at
the *open* stage rather than during a query. `device_cgroup_rules: ['a *:* rwm']` lifts
it and, unlike enumerating `devices:`, keeps working when a USB drive appears at a new
letter. The exposure is the same as the mount you already have: `/dev` was never hidden,
only unopenable.

**2. `CAP_SYS_ADMIN` — needed once `open()` succeeds.** A native NVMe read goes through
`NVME_IOCTL_ADMIN_CMD`, which the kernel guards with `capable(CAP_SYS_ADMIN)`, and Docker
drops it by default — so an NVMe drive fails with `EPERM` even as root.

**3. `CAP_SYS_RAWIO` — for anything behind a USB bridge.** Those are driven with `SG_IO`
passthrough, and the kernel refuses a passthrough command that is not on its read-only
allow-list unless the caller holds `CAP_SYS_RAWIO` — again surfacing as `EPERM`:

```
Read NVMe Identify Controller failed: Operation not permitted
```

The signature of this one is a **USB enclosure failing while an internal disk reads
fine**. The internal disk uses the NVMe ioctl (gate 2) and the USB one uses passthrough
(gate 3), so they need *different* capabilities — which is genuinely counter-intuitive
and the reason a single-drive test can mislead you.

Each gate masks the ones after it, so fix them together. Adding capabilities one at a
time produces no visible change until the cgroup is open, which is the confusing part.

To confirm which gate you are on, bypass smartctl entirely:

```bash
docker exec smart-serve-dev dd if=/dev/nvme0n1 of=/dev/null bs=512 count=1
```

`Operation not permitted` from `dd` is the cgroup. `dd` succeeding while `smartctl` fails
is a capability — and *which* capability depends on the transport: `smartctl -i` failing
on `/dev/nvme0n1` is gate 2, while a USB device failing with the internal disk reading
fine is gate 3.

The UI reports this too: open the drive and use **Show raw output** — it prints what
`smartctl` said, verbatim.

Caveat: device nodes appear and disappear with hotplug (a node opened mid-scan after
removal can error — handled per scan, retried next scan).

### Option B — enumerated `--device` passthrough (least privilege)

```bash
docker run -d \
  -p 8080:8080 \
  -v smartserve-data:/data \
  --device=/dev/sda --device=/dev/sdb --device=/dev/nvme0 --device=/dev/nvme0n1 \
  --cap-add SYS_ADMIN \
  ghcr.io/masieono/smart-serve:latest
```

Most secure, but device letters shift across reboots and USB plugs — only practical
for stable internal disks. Not recommended for the USB use case.

`--device` adds the device cgroup rule for you, so no `device_cgroup_rules` is needed
here. It does **not** grant capabilities, so `SYS_ADMIN` (native NVMe) and `SYS_RAWIO`
(USB bridges) are still required — and enumerate the NVMe *controller* as well as its
namespace.

### Option C — `--privileged` (works, but unnecessary)

`--privileged` grants every capability, including the two that Option A adds explicitly.
Since those are the only ones the app needs, the rest buy nothing. Listed for
completeness; discouraged.

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

## Updating an existing install

The image is published as `latest` and as the version. On the host:

```sh
cd /path/to/your/compose/file
docker compose pull
docker compose up -d
docker compose logs --tail=20 smart-serve   # the startup probe line
```

`docker compose pull` is the step that matters: `docker compose up -d` alone reuses the local image,
so an install that skips the pull keeps running whatever it already had.

Check the sidebar footer says the new version. That is the one thing the tag proves, and a
`0.0.0-dev` there means the image was built without a `VERSION` build-arg. To pin rather than track
`latest`, set `image: ghcr.io/masieono/smart-serve:1.1.0`.

**Nothing else is required.** The database migrates itself on start, and the changelog says when a
release adds migrations. To roll back, pin the previous tag and pull again — the schema is additive,
so an older binary ignores columns it does not know about.

### One thing an upgrade can look like a missing feature

A release that starts **reporting something new** cannot show it for data that was never collected,
and the app will not invent it. The partition schema (`A28`) is the first example: `lsblk` has always
been asked for the partitions, but until `1.2.0` their sizes, labels and usage were thrown away rather
than stored, so every drive scanned by an earlier version has no layout recorded.

**Press Scan once after such an upgrade** and the card appears. This is not a fault and there is
nothing to configure — the scan is the same on-demand scan as always, and it fills the new column for
every drive it touches. A drive that has never been scanned since the upgrade simply shows no
partition card, which is the honest state: the app has not read it yet.

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

Smart-Serve itself needs **no credentials** — no login, no database password, no API key. The
only secrets it handles are **notification channel credentials**: ntfy access tokens, Gotify
app tokens, Telegram bot tokens, Discord webhook URLs, SMTP passwords, and any authorisation
header a generic webhook needs. Use no notification channels and there are none.

A channel's fields come from three places, with precedence **environment > mounted file >
database** (`ADR-013`, `ADR-022`):

1. **The environment**, including a Compose `env_file:` or `.env` file. Note that *Docker
   Compose* reads `.env` and injects it; the app never parses `.env` itself.
2. **A mounted file**, through the `_FILE` form of the same variable — the Docker-secrets
   convention, which keeps the value out of the compose file, out of the process environment,
   and out of `docker inspect` output.
3. **The database**, set in the web UI. Convenient, no restart needed, and where a value lands
   when neither of the above supplies it.

#### Naming

Variables are named after the channel's **label**, because a label is the only identifier a
compose file can know — an id is assigned on insert, and changes if a channel is deleted and
recreated:

```
SMARTSERVE_CHANNEL_<LABEL>_<FIELD>
SMARTSERVE_CHANNEL_<LABEL>_<FIELD>_FILE
```

`<LABEL>` and `<FIELD>` are uppercased with each run of non-alphanumerics reduced to a single
underscore. So a channel labelled `Critical alerts` with a `token` field reads
`SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN`:

```yaml
# Straight from the environment, or from a gitignored .env beside this file:
environment:
  SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN: tk_abc123

# ...or from a mounted file, so the value never appears in the compose file at all:
environment:
  SMARTSERVE_CHANNEL_WEEKLY_DIGEST_TOKEN_FILE: /run/secrets/ntfy_digest
secrets:
  - ntfy_digest

secrets:
  ntfy_digest:
    file: ./ntfy_digest_token        # this file contains the token and nothing else
```

**Every string field is overridable, not only the secret ones.** One mechanism therefore
covers both "keep the token in a Docker secret" and "point this channel at a different ntfy
server per environment".

**The editor shows the variable names.** Open a channel and the expected prefix is printed
beneath the label; any field the deployment supplies is badged *from environment* or *from
file* rather than looking blank. This matters because the name is derived from the label:
**renaming a channel changes which variables it reads**, and the badge makes that visible
instead of leaving you to discover it when alerts stop arriving.

**A broken secret file is an error, never a fallback.** If a `..._FILE` variable points at a
missing or empty file, the send fails and the delivery log records why. It does not quietly
fall back to the stored value — that would send a stale token while you believed you had
rotated it.

**Values are never sent to the browser.** The API reports which variables are *set*, not what
they hold. A resolved secret handed to the page would leak into it, and the next Save would
write it into the database — promoting a secret that was meant to stay out of it.

#### What this means for your data volume

Anything not supplied by the environment or a file lives in `/data/smartserve.db`, and
therefore in whatever you back that volume up with. If that includes a NAS or a cloud target,
use the environment or a mounted file for the tokens. See `.env.example` and `ADR-022`.

### Licensing

The application source is **MIT**. The image bundles **smartmontools** (`smartctl`),
which is **GPLv2**; the image ships smartmontools' license text and a pointer to its
corresponding source. The app calls `smartctl` as a separate process, so the two are
aggregated, not linked (`ADR-012`).

## Verifying the deployment

1. `docker logs smart-serve` — startup should log schema version and scheduler
   state, then go quiet. It also runs **one device-access probe** and logs the result,
   so a blocked container says so here instead of only showing an empty dashboard:
   `S.M.A.R.T. reads are blocked, not unsupported` with the cause and the compose fix.
2. Open the UI → press **Scan** → every `lsblk` disk appears.
3. `docker stats smart-serve` — after the scan settles, CPU ~0%, memory <20 MB
   (without an open tab). This is the acceptance test for the idle-frugality
   invariant.

### The image's own health check

The image ships a `HEALTHCHECK` that asks the **HTTP server**, not the binary:

```sh
wget -qO- "http://127.0.0.1:${SMARTSERVE_ADDR##*:}$b/healthz" > /dev/null || exit 1
```

It deliberately does not run `smart-serve version`, which exits 0 for a process whose
server is wedged — a container could then sit there reporting healthy while the API was
unreachable. The two expansions are there so a non-default deployment still checks
correctly: the port is taken from `SMARTSERVE_ADDR` (which may carry a host, e.g.
`0.0.0.0:9090`), and `SMARTSERVE_BASE_PATH` is normalised to the same leading-slash,
no-trailing-slash shape the server expects, so subpath hosting (`ADR-015`) is checked at
`/smartserve/healthz` rather than silently 404ing.

Check it directly with `docker inspect --format '{{.State.Health.Status}}' smart-serve`,
or hit the endpoint yourself: `curl -fsS http://localhost:8080/healthz` → `ok`.

### Build context

`.dockerignore` keeps `.git`, local `data/`, stray `*.db` files and build output out of
the build context. Without it, `COPY . .` ships the entire working tree — including a
developer's database — into the builder. It is scoped so that `internal/webui/assets/**`
and `internal/store/migrations/**`, the only things the build actually embeds, are never
dropped.

### Which revision is this container running?

The version is stamped into the binary at build time (`-X main.version`) and shown once,
in the sidebar's brand mark. It is **not** a constant, so it can answer the question the
version display exists for.

Because `.git` is deliberately not in the build context, the caller supplies the value:

```bash
make docker                                    # stamps `git describe` automatically
# or, without make:
docker build --build-arg VERSION=$(git describe --tags --always --dirty) -t smart-serve:dev .
# or via compose:
SMARTSERVE_VERSION=$(git describe --tags --always --dirty) \
  docker compose -f docker-compose.dev.yml up --build
```

`git describe` yields a tag (`v1.2.0`) once tags exist and a bare commit id before then —
either way it identifies the build. A build with no `VERSION` says `0.0.0-dev`, which is
the honest answer rather than a release it is not.

## Local development & live preview

So the app can be developed and previewed outside a deployment host:

- **`./update.sh`** is the loop for a host that *builds* the app rather than pulling a published
  image: `git pull --ff-only`, rebuild, restart, with `git describe` stamped in so the sidebar says
  which revision is running. `--no-pull` rebuilds what is checked out; `--logs` follows the log
  after it comes up. A host running the GHCR image does not need it — `docker compose pull` is the
  whole of that.
- **`docker-compose.dev.yml`** builds the image from the working tree and stamps the version, so a
  rebuild picks up changes without publishing.
- **Go**: run `go run ./cmd/smartserve` directly on a Linux dev box for the fastest
  loop (device access still requires permissions on `/dev`). Frontend changes need
  only a browser refresh — the assets are static and embedded at build time in
  release builds, served from disk in dev mode.
- **CI (GitHub Actions)**: on push — `go vet`, unit tests (interpreter fixtures),
  then a multi-arch `buildx` build; on tag — push to `ghcr.io/masieono/smart-serve`.
- **Preview**: the dashboard is reachable at `http://localhost:8080` from the dev
  compose stack; a private-repo image is not pullable by others until the GHCR
  package is made public (D21).

### A Raspberry Pi as a development host

**Verified in part on a Pi 5 running DietPi, as of 2026-11-08.** The development container runs
natively on arm64, reports healthy and reads the attached USB SATA disk's full SMART data through
SAT. An app scan also shows the matching model, serial and 17 attributes, with the selected raw
values verified. A short test completed without error, but the automatic UI completion check
failed: 90% left and **Stop test** persisted after waiting/reloading. That capture — a completed live
API result beside stale saved progress and an unread-log flag — **identified the bug, which is now
fixed**: the flag was raised for a log the app reads, and the card no longer skips its live read.
The same symptom is reported on amd64, so it was never ARM-specific. **Scan now** restored idle
controls and successful history, persisting after reload. The SD-card page also flickered and
repeatedly reported `0/1 readable` scans, which pinned a second defect to the staleness re-read —
**also fixed**, now one attempt per refresh window per drive. Both need a retest on the Pi.
Stability checks remain pending. Results and the next check are in
[TESTING.md Pass 8](TESTING.md#pass-8--arm64-raspberry-pi).

Use a 64-bit Linux installation. Check `uname -m` (`aarch64`), `getconf LONG_BIT` (`64`), and
the Docker server/image architecture; the image must be `linux/arm64`. No ARMv7 image is published.
If the test container is already deployed, continue the test record without updating it first.

**1. Docker.** On DietPi: `dietpi-software`, then *Software Optimised* → **Docker** and *Install*.
Or the official script, which is what DietPi runs underneath:

```sh
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"   # then log out and back in, or use sudo for now
```

**2. The repository.**

```sh
git clone https://github.com/Masieono/smart-serve.git
cd smart-serve
./update.sh
```

That builds and starts the development stack natively on the Pi using the repository's Dockerfile.
It is a local image, not the published GHCR artifact. The dashboard is on `http://<pi-address>:8080`.
Record its image ID as well as the reported version: `update.sh` currently calculates the version
before pulling, so the version string alone cannot prove which source revision was built.

**Development restart behavior:** `docker-compose.dev.yml` has no automatic restart policy;
the Pi inspection confirmed `restart=no`. The production Compose file uses `unless-stopped`.
Reboot/recovery testing is still pending; a healthy development container does not establish
automatic startup after a reboot.

**3. Device access.** The captured deployment has `/dev:/dev:ro`, the device-cgroup rule,
`SYS_ADMIN` and `SYS_RAWIO`. Its startup probe succeeded on `/dev/sda`, and smartctl discovered
that disk as `sat` / `ATA`. A subsequent `smartctl -x --json -d sat /dev/sda` returned exit 0,
the WDC identity and 17 ATA attributes. The operator then verified these identity and attribute-table
checks after an app scan, with no apparent errors. A short self-test starts and displays periodic
status updates; the drive and live API subsequently reported successful completion. The app
failed to clear its running state after waiting/reloading. **Scan now** then cleared the progress
bar, restored short/long test buttons and displayed the successful history entry; these results
persisted after reload. That stale state was a bug, and the capture that pinned it is in
[TESTING.md Pass 8](TESTING.md#pass-8--arm64-raspberry-pi): the app raised an unread-self-test-log
flag for a log it reads, and the card skipped its live read because of it. Both halves are fixed.
The attached SD card is not expected to provide S.M.A.R.T. Its app page is visible, but opening
it caused flicker and repeated `scan complete: 0/1 readable` toasts — a second bug, in the
staleness re-read, also fixed: one attempt per refresh window per drive. The API reports
`Unable to detect device type`, health/cause `unknown` and `last_scan: null`, which is what kept
the re-read firing: only a *successful* read advances that timestamp. The detail-page notice itself
has not been checked.

Container `lsblk` reports the USB disk as `Dual SATA Bridge` without a serial, while host `lsblk`
reports the WDC drive identity. The container's full smartctl read returns the matching WDC model
and serial, and the operator confirmed both in the app. Container `lsblk` also reports
container mountpoints rather than the host's `/` and `/boot/firmware`. For storage-usage testing,
the reference mounts do not expose the host's filesystem layout at its original paths.

**4. Continue one test at a time.** The container SMART read and app identity/attribute checks are
complete for this disk, and a short-test completion is confirmed in the drive log. The app's
running-state check failed after both waiting and reloading, despite a completed live API response.
The saved record captured old 90% progress and the unread-log flag that suppressed the initial live
read — **the bug, now fixed**; and opening the SD-card page then failed the no-repeat-scan check,
with the null successful-scan timestamp and rapid failed reads pinning it to the staleness re-read —
**also fixed**. Both need a retest here; the retest steps are in
[TESTING.md Pass 8](TESTING.md#pass-8--arm64-raspberry-pi). Pass 8 next checks dashboard health and
the disconnected-device filter against the saved API state. Neither symptom was ARM-specific: a
similar self-test report comes from amd64.
Resource testing remains open: the first process RSS sample was **21.97 MiB**, and Docker's
`0B / 0B` memory display was unusable. Measure process RSS from `/proc/1/status`, separately from
Docker container memory, and establish a controlled idle baseline before claiming compliance.
