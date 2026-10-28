# Releasing Smart-Serve

The checklist for cutting a release, and the record of what is deliberately left undone at
each one. Written for v1.0.0; reusable afterwards.

## Cutting a release

1. **Land the work on `main`** and let CI go green. `make ci` runs the same four checks
   locally (see the README's Development section).
2. **Update `CHANGELOG.md`** — move entries out of "unreleased" and date them. The same text
   is the GitHub Release body.
3. **Tag it:** `git tag -a v1.0.0 -m "v1.0.0"` then `git push origin v1.0.0`. CI builds amd64
   and arm64, pushes `1.0.0` and `latest`, and stamps the version into the binary.
   **A tag containing a hyphen is treated as a pre-release and does not move `latest`** —
   `v1.1.0-rc1` publishes `1.1.0-rc1` only.
4. **Create the GitHub Release** from the tag, pasting the changelog section as the body.
5. **Check the published image reports the right version** in the sidebar. That is the one
   thing the tag exists to prove, and a `0.0.0-dev` there means the `VERSION` build-arg did
   not reach the build.

## Before the first public release (v1.0.0)

- [x] Repository renamed to `masieono/smart-serve`.
- [x] Documentation reconciled with the code: the image base (`ADR-020`), and notification
      secret handling — `ADR-021` recorded the gap, and `ADR-022` built the mechanism rather
      than shipping a description of its absence.
- [x] **The GHCR package is public.** A *separate* switch from repository visibility
      (**Package settings → Danger Zone → Change visibility**). While it is off,
      `docker compose pull` fails for everyone who has not authenticated — which includes
      everyone reading the README for the first time. Re-check it without credentials:
      `curl -s 'https://ghcr.io/token?scope=repository:masieono/smart-serve:pull&service=ghcr.io'`
      returns a token when the package is public and `401` when it is not.
- [x] Image size measured and published: **14.8 MB content, 53.5 MB on disk**
      (`docker image ls` on the homelab host, 2026-10-17). Recorded in `README.md`,
      `DEPLOYMENT.md` and `ADR-020`.
- [ ] Add screenshots (below). **The only open item on this list.**
- [x] Tag `v1.0.0` and create the Release.

## Screenshots to take

Needed for the README and for the release announcement. In priority order:

1. **Dashboard** with several drives attached — verdict badges, health gauges, and the
   clickable health summary beside the search box. This is the hero shot.
2. **Device page, top half** — verdict, score and grade, the identity grid, temperature.
3. **Attribute table with a `?` explanation open** (`FEATURES.md` A12). This is the most
   distinctive thing the app does, and it is invisible in every other screenshot.
4. **Trend sparklines** with a hover readout showing value, date and time.
5. **A notification** — either the message as received, or the channel editor.

Store them in `docs/images/` and reference them from `README.md`. Keep them at a consistent
width and theme; the dark theme shows the palettes off best.

## Image size

Measured for v1.0.0: **14.8 MB content, 53.5 MB on disk** (`docker image ls` on a homelab
host). Published in `README.md`, `DEPLOYMENT.md` and `ADR-020`.

Re-measure for any release that changes the base image or adds a runtime dependency:

```sh
docker compose pull
docker image ls ghcr.io/masieono/smart-serve
```

Publish **both** figures and say which is which: the compressed one is what a user downloads,
the on-disk one is what their server stores. They differ by more than a factor of three, which
is exactly how a "well under 20 MB" claim survived unchallenged for the life of the project —
it was true of one of them.

## Verifying a release

```sh
docker run --rm ghcr.io/masieono/smart-serve:latest version   # the stamped version
docker compose pull && docker compose up -d
docker compose logs --tail=20 smart-serve                     # startup probe line
```

The startup probe is the useful one: it reads a single drive at boot and logs which of the
three device-access gates is closed if it cannot (`DEPLOYMENT.md`).

## arm64 verification (Raspberry Pi)

CI builds both architectures on every pull request, so an arm64 *build* failure is caught
before release. That is not the same as arm64 *working* — nothing has run on ARM hardware
yet. A Raspberry Pi is the cheapest way to close that, and the useful test is small.

**What you need**

1. **A 64-bit Raspberry Pi OS.** Check with `uname -m` — it must say `aarch64`. A 32-bit
   userland will pull the arm64 image and fail to `exec` it, which presents as a crash rather
   than as the architecture mismatch it is. This is the single most common way to lose an
   afternoon here.
2. **A drive in a USB enclosure.** The Pi's own storage is an SD card, which reports no
   S.M.A.R.T. at all, so it is not the interesting device. An SSD or HDD on USB is: it
   exercises the `SG_IO` passthrough path and the `CAP_SYS_RAWIO` requirement, which are the
   parts most likely to differ from x86.
3. **The reference compose file, unchanged.** If the three device-access gates need something
   different on ARM, that is precisely the finding worth having, and changing the compose file
   first would hide it.

**What to check**

- A scan finds the USB drive, and the device page shows its attributes rather than a notice.
- `docker compose logs` shows the startup probe reading a drive successfully.
- **Idle CPU and RSS.** A Pi is where the memory figure genuinely differs from an x86 server,
  and it is the figure the ARM audience will want — the SD card as the boot device makes the
  Pi a plausible always-on host for exactly this app.

**When to do it**

After v1. It verifies an architecture that already builds, and it needs hardware rather than
code. Nothing in the release depends on it; the honest position until then is the one already
recorded in `SCOPE.md` — arm64 is built by CI and has never been run.
