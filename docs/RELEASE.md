# Releasing Smart-Serve

The checklist for cutting a release, and the record of what is deliberately left undone at
each one. Written for v1.2.0; reusable afterwards.

## Cutting a release

1. **Land the work on `main`** and let CI go green. `make ci` runs the same four checks
   locally (see the README's Development section).
2. **Update `CHANGELOG.md`** — move entries out of "unreleased" and date them. The same text
   is the GitHub Release body.
3. **Tag it:** `git tag -a v1.2.0 -m "v1.2.0"` then `git push origin v1.2.0`. CI builds amd64
   and arm64, pushes `1.2.0` and `latest`, and stamps the version into the binary.
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
- [x] Add screenshots (below). **Done** — cropped to the app viewport, in `docs/images/`.
- [x] Tag `v1.0.0` and create the Release.

## At v1.1.0

The last of the roadmap: scheduled self-tests, group-scoped channels, per-model thresholds, and the
side-by-side comparison. `SCOPE.md` Phase 4 is complete and `FEATURES.md` §H has nothing left open.

**Deliberately left undone, and where each is recorded:**

- **`D7` localization.** Deferred, not dropped: the cost is extracting every string and keeping every
  locale in step, which is only worth doing for a translator who will maintain the result
  (`ADR-023`, `SCOPE.md`).
- **Error-log entry *lists* are not rendered.** The counts are, and the self-test entries are, but
  smartctl emits no entry list at all when a log is empty — and neither drive available to this
  project has ever logged an error, so the *populated* shape has never been observed. Guessing it
  would produce a panel that renders nothing precisely when a drive has something to say. The raw
  document is stored verbatim and the panel says so, so nothing is lost; rendering lands when a
  populated log is captured (`FEATURES.md` A20). **Built at v1.2.0, against a fixture; see below.**
- **An attribute row cannot be marked "the verdict blamed this".** `interp.Penalty` carries a
  free-text reason but no attribute id, so the attribute table can only mark rows the *drive itself*
  flags. Adding an optional id would make it exact and retire the last UI-side judgement
  (`FEATURES.md` A12). **Built at v1.2.0; see below.**
- **arm64 hardware verification was outstanding at this release.** Updated 2026-11-08: a Pi 5 /
  DietPi deployment confirms native execution, startup health, a full container SMART read through
  USB SAT, and the app's identity and attribute-table checks for the tested disk. That session
  surfaced two defects — a self-test progress bar stuck at a percentage the drive's own log had
  moved past, and an SD-card page that re-read itself in a loop — **both fixed, both awaiting a
  retest on the Pi**. Controlled idle measurements, stability and broader parity remain open; see
  [the current test record](TESTING.md#pass-8--arm64-raspberry-pi). **Idle was measured at v1.2.0.**
- **No tag-size re-measurement.** v1.1.0 adds no runtime dependency and does not change the base
  image, so the v1.0.0 figures still hold. The rule stands: re-measure for any release that changes
  either.

**What the tag is for.** `make ci` green, the changelog section as the release body, and then the one
thing a tag proves — that the published image reports *that tag's version* in the sidebar rather than
`0.0.0-dev`. A `0.0.0-dev` there means the `VERSION` build-arg did not reach the build.

## At v1.2.0

The interface round. A question about reporting partition sizes turned into an audit of what the app
fetches and then discards, and three things that were being paid for on every scan and never shown are
now shown (`A28` partition schema and storage usage, `A12` score breakdown, `in_smartctl_database`).
The same release rebuilt the front end as one shell over five pages (fleet, device, schedules,
notifications, settings): the card grid, the replace-first shortlist and the separate page bodies are
gone, and the fleet is a table with a search box. Optional single-user sign-in arrives with sessions,
a logout and an idle timeout (`ADR-033`), per-model thresholds become a table, and a drive's own error
log is rendered. The ARM pass turned up two long-standing defects, a self-test progress bar stuck on a
stale percentage and a drive page that re-read an unreadable drive forever, both fixed here. Three
additive migrations (`0015`-`0017`); no schema rewrite, so the v1.1.0 rollback note still holds.

**Deliberately left undone, and where each is recorded:**

- **`D7` localization.** Unchanged from v1.1.0: deferred, not dropped, and still waiting on a
  translator who will maintain the result (`ADR-023`, `SCOPE.md`).
- **Error-log entries other than the fixture have never been seen.** The panel and the parser are
  built and checked against `internal/smart/testdata/ata-error-log-entries.json`, reconstructed from
  smartctl's documented shape because no drive available to this project has ever logged an error. The
  panel is fixture-verified: it renders what that shape says it will, and a drive with real errors is
  the confirmation still owed (`FEATURES.md` A20).
- **RAID / LVM / ZFS member naming is built and unit-tested but never exercised on real hardware.** The
  no-partition-table path names a disk by its `blkid` signature (`linux_raid_member` and so on), and
  the fixtures prove the naming, but nothing has pulled a disk from an array and opened its page
  (listed under "Not covered here" in `TESTING.md`, `FEATURES.md` A28). This is the weakest claim in
  the release.
- **NVMe remains unverified on hardware.** No NVMe device is attached to the ARM host, so its
  temperature, health fields and amd64 parity rest on the parser and its fixtures (`TESTING.md` 8.6).
- **arm64 idle budget: measured, 2026-11-18.** Ten minutes of samples with nothing else running give
  about **20 MiB** both as RSS and as PSS, 11 file descriptors and 11 threads flat, and roughly **0.04%
  CPU** on a Pi 5. The cgroup cannot account memory on that host (no memory controller; `docker stats`
  reads `0B / 0B`), so the criterion is stated against **PSS**, which is the better figure anyway: it
  does not double-count shared pages. The amd64 host reads 18.9 MiB by the cgroup and agrees.
  `TESTING.md` 8.5 carries the samples.
  The two defects this host found are fixed and **retested on 2026-11-11**, on both architectures.
- **Image size: re-measured for v1.2.0.** **14.7 MB content, 53.6 MB on disk** on amd64, and **60.7 MB
  on disk** on arm64, on 2026-11-25 from images built at `41a45b0`. That is within rounding of v1.0.0's
  14.8 / 53.5, which is what "no base change and no new runtime dependency" was supposed to mean, so the
  figures in `README.md` and `DEPLOYMENT.md` (about 15 MB compressed, 54 MB on disk) still hold.
  `Dockerfile` still installs `smartmontools` and `util-linux` **unpinned**, so this is a measurement to
  repeat, not a guarantee.

**Closed at this release:** an attribute row can be marked as one the verdict *read*, via the score
breakdown's `attribute_id` (the v1.1.0 note asking for it is done, `FEATURES.md` A12); optional
single-user sign-in (`ADR-033`; no lockout, and no TLS of its own); per-model thresholds as a table
(`FEATURES.md` A3); the drive error-log panel (`A20`); and the single interface over five pages.

**Consider a release candidate first.** `v1.2.0-rc1` publishes `1.2.0-rc1` without moving `latest`
(step 3 above), which is how the Pi can retest the two fixes against a *published* artifact rather
than the local build used on 2026-11-11. Tag `v1.2.0` once that retest passes.

## Screenshots

The README's set, cropped to the app viewport and committed to `docs/images/`:

| File | Shows |
|---|---|
| `fleet.png` | The fleet: one row per drive, verdict and score in one cell, usage, temperature, powered-on hours |
| `device-page.png` | A drive page: verdict, facts, disk space, trends, attributes, self-test |
| `partitions.png` | Disk space: the usage line, the disk map and the partition table, linked both ways |
| `attribute-explanation.png` | A `?` explanation open over the attribute table |
| ~~`dashboard.png`~~, ~~`selftest-card.png`~~ | **Removed with the interface they showed.** Both were parts of the dashboard that no longer exists; the shots above replace them. |

**Take them as viewport captures, not full screens**, so no cropping is needed — Firefox's right-click
*Take Screenshot* → *Save visible*, or Chrome DevTools → `Ctrl+Shift+P` → *Capture screenshot*. Keep
them at one consistent window size (the committed ones are 1900×965) and on the dark theme, which
shows the palettes off best. Full-screen captures with browser chrome in them go in
`temp screenshots/`, which is gitignored and keeps them out of the history.

## Image size

Measured for v1.2.0 on 2026-11-25, built at `41a45b0`:

| | content (compressed) | on disk |
|---|---|---|
| amd64 | **14.7 MB** | **53.6 MB** |
| arm64 | measured at rc1 from the published manifest | **60.7 MB** |

The two architectures differ by 7 MB on disk because the arm64 base layers are larger, and the figures
published for v1.0.0 (14.8 / 53.5) were amd64 only. `README.md` and `DEPLOYMENT.md` say "about 15 MB
compressed, 54 MB on disk", which is right for amd64 and understates arm64's disk usage.
Measured for v1.0.0: **14.8 MB content, 53.5 MB on disk** (2026-10-17), recorded in `ADR-020`.
`README.md` and `DEPLOYMENT.md` publish the rounded form of both figures. The number changes when the
base image or a runtime dependency changes, and `Dockerfile` pins neither `smartmontools` nor
`util-linux`, so every release measures rather than assumes.

Re-measure for any release that changes the base image or adds a runtime dependency:

```sh
docker build -q --build-arg VERSION=v1.2.0 -t smart-serve:sizecheck .
docker image ls smart-serve:sizecheck --format '{{.Size}}'            # on disk
docker image inspect smart-serve:sizecheck --format '{{.Size}}'       # content, in bytes
```

Building locally is enough for both figures and does not need the release to be published: the
inspect size is the compressed content size recorded in the image config, and the `ls` size is the
extracted size on disk. Taking both from the same build is what keeps the pair comparable.

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

**Current status (2026-11-18): verified in part on Raspberry Pi 5 / DietPi.** Native `linux/arm64`
execution, the reference device-access configuration, startup probe, HTTP health, USB SAT
auto-detection and a full container SMART read of the attached WDC disk are confirmed, as are the app's
model, serial, 17 attributes and selected raw values after a completed scan. This is not verification of
the published GHCR image, nor full amd64 parity.

The 2026-11-08 session on this host (then `v1.1.0-23-g949fbd6`) turned up two defects, a short test
whose progress never cleared and an SD-card page that re-read itself in a loop. Both were diagnosed
from that session's own captures, fixed, and **retested on 2026-11-11 against `v1.1.0-28-g6fee3dc`, on
this Pi and on amd64, and both pass.** The retest ran against a local build of the fixes rather than the
published image.

**Still open:** overnight stability, NVMe, restart/reboot persistence, and identity across a different
enclosure bay. The idle budget is no longer open: ten minutes of samples on 2026-11-18 give about 20 MiB
RSS and PSS, 11 file descriptors and 11 threads flat, and roughly 0.04% CPU (8.5). This host still
cannot report a cgroup memory figure, so the criterion is stated against PSS.

[TESTING.md Pass 8](TESTING.md#pass-8--arm64-raspberry-pi) holds the dated evidence, image IDs and
per-check statuses. Keep that record authoritative rather than duplicating the checklist here.

For further verification:

1. Use a 64-bit Linux installation and confirm the Docker image is `linux/arm64`.
   `uname -m` checks the kernel architecture only; also check `getconf LONG_BIT` and the
   Docker server/image architecture. The published ARM platform is arm64, not ARMv7.
2. Use a SMART-capable USB SSD/HDD, with optional NVMe coverage when that hardware is available.
   The Pi's SD card is a no-SMART control, not evidence that USB SMART works.
3. Keep the current deployment fixed for the baseline. Record any configuration workaround
   separately, and identify the image actually tested. A local source build and a published image
   need separate provenance records.

CI builds both architectures on pull requests, but its application tests run on x86;
there is no ARM application/device test in the workflow. Continue the hardware checks one at a time.
