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
  populated log is captured (`FEATURES.md` A20).
- **An attribute row cannot be marked "the verdict blamed this".** `interp.Penalty` carries a
  free-text reason but no attribute id, so the attribute table can only mark rows the *drive itself*
  flags. Adding an optional id would make it exact and retire the last UI-side judgement
  (`FEATURES.md` A12).
- **arm64 hardware verification was outstanding at this release.** Updated 2026-11-08:
  a Pi 5 / DietPi deployment now confirms native execution, startup health and a full container
  SMART read through USB SAT. App identity and attribute-table checks also passed for the tested
  disk. A short test completed without error, but automatic UI completion failed: 90% left
  persisted after waiting/reloading despite a completed live API response. That capture — saved
  progress and an unread-log flag beside a completed live result — **identified the bug, which is
  fixed**: the flag was raised for a log the app reads, and the self-test card no longer skips its
  live read. **Scan now** restored idle controls and successful history, persisting after reload.
  The same self-test symptom is reported on amd64, so it was never ARM-specific.
  Opening the Pi's SD-card page also caused flicker and repeated `0/1 readable` scan toasts. The
  API's `last_scan: null` and five failed device reads in the same displayed second pinned it to
  the staleness re-read — **also fixed**, now one attempt per refresh window per drive. Both fixes
  need a retest on the Pi. Controlled idle measurements, stability and broader parity remain open;
  see [the current test record](TESTING.md#pass-8--arm64-raspberry-pi).
- **No tag-size re-measurement.** v1.1.0 adds no runtime dependency and does not change the base
  image, so the v1.0.0 figures still hold. The rule stands: re-measure for any release that changes
  either.

**What the tag is for.** `make ci` green, the changelog section as the release body, and then the one
thing a tag proves — that the published image reports `1.1.0` in the sidebar rather than `0.0.0-dev`.

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

**Current status (2026-11-08): partial hardware verification on Raspberry Pi 5 / DietPi.**
Native `linux/arm64` execution, the reference device-access configuration, startup probe,
HTTP health, USB SAT auto-detection and a full container SMART read of the attached WDC disk are
confirmed. The operator also verified the app's model, serial, 17 attributes and selected raw
values after a completed scan, with no apparent errors. The local development image reports
`v1.1.0-23-g949fbd6`. This is not verification of the published GHCR image or full amd64 parity.
A later direct read confirmed a short self-test completed without error. Automatic UI completion
failed: 90% left and **Stop test** persisted after waiting/reloading. The live API returned the
completed history with no `running` field, while the saved record contained 90% progress and an
unread-log flag — and **that pairing was the bug, which is now fixed**: the flag was raised for a
log the app reads (`ata_smart_selective_self_test_log`), and the card skipped its live read because
of it. **Manual recovery passed:** after **Scan now**, idle controls and successful history returned
and persisted after reload. The same symptom is reported on amd64, so an ARM-specific cause was
never in play. A second failed check concerns the SD-card page: opening it caused flicker and
repeated `0/1 readable` scan toasts. The captured `last_scan: null`, device-type detection error and
five failed device reads in the same displayed second pinned that to the staleness re-read —
**also fixed**, now one attempt per refresh window per drive. This second symptom has only been
reported on the Pi. Both fixes need a retest on this hardware.

[TESTING.md Pass 8](TESTING.md#pass-8--arm64-raspberry-pi) holds the dated evidence, image ID,
individual test statuses and the single next check. Keep that record authoritative rather than
duplicating the checklist here. A first process RSS sample is **21.97 MiB**, above the stated
under-20-MB target; controlled idle conditions and overnight stability have not been measured.

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
