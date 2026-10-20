# Smart-Serve — Architecture Decision Records

Format: context → decision → alternatives considered → consequences. Numbered;
never rewritten (superseded with a new ADR that references the old one).

## ADR-001: Implementation language — Go

**Context.** Hard requirement: minimal idle cost (RAM + CPU), tiny image, no
language-runtime patching burden, in-process scheduler, embeddable frontend.

**Decision.** Implement the server as a single Go binary; frontend embedded via
`embed.FS`.

**Alternatives.**
- *Python/FastAPI* — fastest to develop, richest JSON tooling; but ~60–100 MB
  idle RSS, larger image, and a language runtime to patch. Fails the
  idle-frugality invariant in spirit.
- *Node.js* — familiar ecosystem; ~40–60 MB idle RSS, larger image, same runtime
  concerns.
- *Rust* — comparable or better footprint than Go; slower development velocity
  for this scope and a steeper contributor bar.

**Consequences.** (+) single static binary, distroless image <20 MB, zero idle
CPU, stdlib HTTP keeps the dependency list tiny. (−) slower initial development;
interpreter/fixtures need Go test discipline. (±) contributors need Go
familiarity.

**Note (status, not a rewrite).** The "distroless image <20 MB" consequence above is
**superseded**: the shipped runtime image has always been Alpine, and the size figure was
never measured. The decision itself — one static Go binary with the frontend embedded — is
unchanged. See `ADR-020`.

## ADR-002: SMART data source — shell out to smartctl

**Context.** Need S.M.A.R.T. data for ATA, NVMe, and USB bridges; correctness
across vendor quirks is the product's core.

**Decision.** Execute `smartctl` (bundled in the image) with JSON output flags
and parse its JSON. Never parse human text output.

**Alternatives.**
- *libatasmart / Go libraries (gosmart)* — links or wraps C libraries; NVMe
  support weak-to-absent, USB bridge handling worse, maintenance riskier than
  smartmontools.
- *Raw NVMe/ATA ioctl implementation in Go* — enormous correctness surface;
  re-implements 20 years of vendor quirk handling. Explicitly rejected.

**Consequences.** (+) smartmontools' vendor-quirk maturity and USB `-d`
transports for free; JSON output is stable and versioned. (−) subprocess per
device per scan (acceptable: sequential, ~5 MB transient each); smartctl
version drift managed by pinning in the image build.

## ADR-003: Persistence — SQLite

**Context.** Need durable storage (notes, snapshots, schedules, channels) with
zero additional daemons.

**Decision.** SQLite, WAL mode, one file at `/data/smartserve.db`; embedded
migrations at startup.

**Alternatives.**
- *JSON file store* — simpler; but concurrent scan-writes vs UI-reads,
  partial-write risk on crash, and unbounded history growth made it fragile.
- *Postgres/MariaDB in a sidecar* — a second container with real idle RAM
  cost; fails the self-containment goal outright.
- *Embedded key/value (bbolt family)* — workable, but relational queries
  (history ranges, notification dedupe) map poorly.

**Consequences.** (+) one file, one process, relational queries, trivial
backup. (−) single-writer serialization (fine at this write rate); driver choice
(cgo `mattn/go-sqlite3` vs pure-Go `modernc.org/sqlite`) resolved at
implementation time — pure-Go preferred for static distroless builds.

## ADR-004: Scheduling — in-process, not host cron

**Context.** Scheduled S.M.A.R.T. scans are a core feature. Host cron would
break self-containment ("zero host setup").

**Decision.** A single scheduler goroutine inside the app; schedules stored in
SQLite; intervals and cron expressions both supported; next-run computed after
each scan.

**Alternatives.**
- *Host cron + `docker exec`* — external moving parts, invisible to the UI,
  violates self-containment.
- *A cron container sidecar (ofelia etc.)* — a second container for zero
  functional benefit.

**Consequences.** (+) UI-managed schedules, no host setup, one container.
(−) scheduler lives/dies with the app (a stopped container doesn't scan — which
is correct).

**Missed-run policy (decided at implementation).** Default `skip`: a schedule whose
next run passed while the process was down is simply rescheduled, so a long outage
produces one scan rather than a burst of catch-up scans. `SMARTSERVE_MISSED_RUN_POLICY=run_once`
fires the overdue run once instead.

The policy is applied **once, at start-up** (`reconcileStartup`), which is the only
place it can be correct. Once the loop is running, a boundary in the past means one
of two things — the timer just came due, or the operator pressed "run now" — and
both must fire. Deciding the policy on every evaluation instead made a due boundary
look like a missed run and pushed it into the future, so due-now schedules and
"run now" silently never fired.

**Idle cost.** The loop sleeps to the next boundary with a single `time.Timer` and
an explicit wake channel, never a ticker (`FEATURES.md` §E). With nothing scheduled
it blocks indefinitely; a schedule change nudges it so edits take effect at once
rather than at the next boundary. Cron is evaluated in the container's local time
using the standard library's own date arithmetic, so DST gaps and repeats are
handled by `time.Date` rather than by hand-rolled arithmetic on instants.

## ADR-005: Device identity — serial number

**Context.** User notes must survive reboot, replug, and device-letter changes.

**Decision.** Serial number is the primary identity (`devices.serial` unique).
Fallback for drives with empty/duplicate serials: `__nomodel:<model>[-<n>]`
disambiguation. Device path is informational only.

**Alternatives.**
- *WWN* — strong, but absent on some USB bridges; serial is near-universal.
- *Model+capacity* — collides in homogeneous drive pools.
- *Filesystem UUID* — wrong layer; partitions/filesystems move between drives.

**Consequences.** (+) notes track physical drives across reboots/replug/letters.
(−) WWN stored for cross-checking; duplicate-serial edge cases get the fallback
with a UI warning.

## ADR-006: Live updates — SSE, not WebSocket, not polling

**Context.** The Scan button should give immediate live feedback per device.

**Decision.** Server-Sent Events over a single `GET /api/events` connection;
browser-native `EventSource` auto-reconnect with `Last-Event-ID`.

**Alternatives.**
- *WebSocket* — bidirectional capability we don't need; extra complexity
  (upgrade handling, heartbeats, proxy pitfalls).
- *Polling* — either wasteful (idle cost) or laggy.

**Consequences.** (+) one-directional, reconnecting for free, trivial to
implement on stdlib; small server-side ring buffer for missed-event replay.
(−) reverse proxies must disable buffering (documented in DEPLOYMENT.md).

## ADR-007: No hot-plug watcher — scans are the freshness mechanism

**Context.** udev monitoring would catch plug/unplug instantly.

**Decision.** No udev/inotify watcher. Device changes are observed only on
on-demand or scheduled scans. This is a deliberate trade, surfaced honestly in
the UI ("last scanned" timestamp on every screen).

**Consequences.** (+) preserves the zero-idle-cost invariant — the core
product promise. (−) hot-plug latency equals time-to-next-scan; mitigated by
the Scan button being one click. Revisit only if user feedback demands it
(would then be an opt-in feature, disabled by default).

## ADR-008: Health-change notifications — transition-based, not state-based

**Context.** A failing drive must alert without spamming every scheduled scan.

**Decision.** `health_change` events fire only on verdict transitions;
persistent bad states stay silent unless a different (worse or recovered)
verdict appears. Squelch windows for repeating conditions (temperature) are
configurable.

**Consequences.** (+) signal, not noise. (−) a missed notification (e.g.
channel down during a transition) is recoverable only via the notification
log — the log records the `failed` delivery, surfaced in the Settings view.

## ADR-009: Configuration strategy — env vars for bootstrap, UI/SQLite for everything else

**Context.** The owner wants sensible defaults that are adjustable in the web UI,
and explicitly does *not* want one environment variable per tunable threshold
(verdict thresholds, temperature, retention, squelch windows, …). At the same time
some settings must exist *before* the database does (listen address, DB path) and
are naturally container-orchestration concerns.

**Decision.** Two tiers:

- **Env vars — bootstrap/operational only** (needed before/around the DB, or
  orchestration-level): `SMARTSERVE_ADDR`, `SMARTSERVE_DB`, `TZ`,
  `SMARTSERVE_MISSED_RUN_POLICY`, `SMARTSERVE_COMMAND_TIMEOUT`,
  `SMARTSERVE_LOG_LEVEL`.
- **Everything else — Settings UI, persisted in SQLite**: every interpretation
  threshold, retention, squelch, maintenance windows, theme, schedules, channels,
  notes.

**Precedence:** an env var provides the *default* for a setting only while that
setting is absent from the DB. Once the user changes it in the UI, the DB value
wins and the env var is ignored (the UI shows the source). Purely operational vars
(`ADDR`, `DB`) are env-only and never stored.

**Alternatives.**
- *All-env configuration* — env-var sprawl; every change requires a container
  restart; poor discoverability. Rejected by the owner.
- *All-UI configuration* — cannot bootstrap the listen port / DB path; a
  bricked DB would leave no way in.

**Consequences.** (+) short `docker run`, discoverable/live-editable settings,
no restart to tune thresholds. (−) two places to look for config — mitigated by
the UI displaying each setting's effective source and default.

## ADR-010: Known Go implementation risks & mitigations

**Context.** The owner is not a Go developer and is trusting the stack choice.
The specific Go-related pitfalls this project must navigate are recorded here so
they are designed for, not discovered.

**Risks & mitigations.**

| Risk | Mitigation |
|---|---|
| cgo SQLite driver (`mattn/go-sqlite3`) breaks static distroless builds and cross-compilation | Use the **pure-Go** `modernc.org/sqlite` driver; accept ~6–8 MB larger binary. Driver hidden behind a tiny `store` interface so it can be swapped. |
| Zombie/hung `smartctl` subprocesses | Always `exec.CommandContext` with a timeout; drain stdout/stderr; always `Wait()`; one scan at a time behind a mutex. |
| Goroutine leaks on long-lived SSE connections | Per-client channel + `context` cancellation on disconnect; bounded ring buffer for replay. |
| Cross-arch builds (amd64/arm64 for NAS/Pi) | Pure-Go dependencies make `GOOS`/`GOARCH` cross-compiles trivial; multi-arch image via `docker buildx`. |
| Large `smartctl --json` payload memory spikes | Stream-decode with `json.Decoder`; bounded buffers; process one device at a time. |
| Time-zone correctness for cron schedules | Load the container `TZ`; evaluate cron in local time; display next-run in the user's zone. |
| Graceful shutdown | Handle `SIGTERM`; stop scheduler; close SQLite cleanly (WAL checkpoint). |
| Runtime footprint concerns | Go GC overhead is negligible at this scale; idle RSS ~10–15 MB measured, not assumed (acceptance test in `SCOPE.md`). |

**Alternatives.** cgo SQLite (faster, but breaks static/multi-arch builds);
linking libatasmart (rejected in ADR-002).

**Consequences.** (+) reproducible, tiny, multi-arch image with no runtime
dependencies. (−) pure-Go SQLite is somewhat slower than the C library — irrelevant
at this write rate; slightly larger binary.

## ADR-011: Drive knowledge base — layered, shipped with releases

**Context.** The owner wants per-drive extras (rated TBW, known firmware advisories)
but correctly flagged that sourcing and maintaining specs for "all or most drives"
is ongoing upkeep that risks becoming a treadmill.

**Decision.** A two-layer knowledge base, with no runtime polling:

1. **Base layer** = smartmontools' upstream `drivedb.h`, which already ships inside
   the `smartctl` binary in the image and is maintained upstream (model families,
   known-issue flags, vendor attribute quirks) — zero effort for this project.
2. **Curated layer** = a small, schema-versioned, bundled JSON table for the extras
   (`drivedb.h` lacks rated TBW/workload, release year, firmware advisories).
   Community-contributed via pull request, shipped with each image release.

The KB is loaded from the binary at startup (no network, no timers). A drive absent
from the KB degrades gracefully: full S.M.A.R.T. reporting continues; only the
extras are missing. A future **opt-in** "refresh knowledge base" action (user-
pressed, never automatic) may address freshness while preserving the idle invariant.

**Alternatives.**
- *Rely solely on `drivedb.h`* — no rated TBW / advisory data; incomplete feature.
- *Runtime fetch from a hosted API* — breaks offline operation, adds a network
  dependency and a privacy consideration, and risks idle cost.
- *Require users to enter specs* — pushes the treadmill onto every user.

**Consequences.** (+) offline-capable, no upkeep automation needed beyond release
cadence, graceful degradation. (−) KB freshness is tied to release cadence — the
opt-in manual refresh is the escape hatch if that proves too slow.

## ADR-012: Licensing — MIT application, GPL-compliant image

**Context.** Personal, non-commercial project to be published on GitHub for others to
use. `smartctl` (smartmontools) is **GPLv2** and is bundled in the image.

**Decision.** The application source is licensed **MIT**. The image bundles the GPLv2
`smartctl` binary; GPL compliance for the *image* is satisfied by shipping
smartmontools' license text and a pointer/offer to its corresponding source
(upstream smartmontools). The app invokes `smartctl` as a **separate process**, not a
linked library, so the app code is not a derivative work of it (mere aggregation).

**Alternatives.** Apache-2.0 (also fine — adds an explicit patent grant and a NOTICE
file; trivial to switch to before the first release). GPL for everything (unnecessary
copyleft on our own code). No license (hostile to users).

**Consequences.** (+) maximally easy for others to use, test, and fork — matching the
owner's intent. (−) the image build must include smartmontools' notices — a one-line
build step, not a burden.

## ADR-013: Secrets — env / .env / *_FILE / DB, env wins

**Context.** The owner asked whether secrets (ntfy tokens, webhook auth headers)
should live in a `.env` file or in SQLite.

**Decision.** Support all three sources, with precedence
**environment > mounted secret file > database**:

1. **Env vars** — including Docker Compose `env_file:` / `.env` (Compose injects them;
   the app does *not* parse `.env` itself).
2. **`*_FILE` variables** — e.g. `SMARTSERVE_NTFY_TOKEN_FILE=/run/secrets/…`
   (Docker-secrets convention) for keeping secrets off the command line.
3. **Database** — set via the UI for convenience (non-technical users, no restart).

The UI displays each secret's source. Precedence follows `ADR-009` (env supplies the
value; the DB value is used only when no env/file value exists).

**Alternatives.** DB-only (secrets then travel in the `/data` volume and its
backups). Env-only (restart to change; awkward for non-technical users).

**Consequences.** (+) a secure path *and* a convenient path; (−) two mechanisms to
document — mitigated by the UI surfacing the effective source.

## ADR-014: Frontend & theming — no framework, token-based themes

**Context.** A sleek, clean, navigable UI with a theme picker is a first-class
requirement; prior iterative UI work drifted and regressed.

**Decision.** Vanilla ES modules + a single CSS token sheet + component styles; no
framework, no build step, no CDN. Theming is a token swap over CSS custom properties
(`light` / `dark` / `auto` / `custom` accent). The server renders the theme attribute
to avoid flash. `UI.md` is the binding contract for all UI work.

**Alternatives.** React/Vue (build step, dependency churn, larger bundle); Tailwind
(build step); a component library (opinionated look, bundle cost).

**Consequences.** (+) fast, offline, tiny, and structurally resistant to UI drift;
(−) more hand-written DOM code, offset by the small, well-specified component set.

## ADR-015: Reverse proxy, base path, and CSRF

**Context.** The owner runs an Nginx reverse proxy and asked how it affects the app.

**Decision.** The app is **proxy-agnostic** — mapping a domain to `IP:port` requires
**no application change**. Three details are handled so proxies work well:

1. **SSE** must pass through unbuffered (`proxy_buffering off`, HTTP/1.1) —
   documented in `DEPLOYMENT.md`.
2. **Subpath hosting** is supported via `SMARTSERVE_BASE_PATH` (e.g. `/smartserve/`),
   so assets, links, and redirects stay prefix-correct.
3. **CSRF**: state-changing endpoints (POST/PUT/DELETE) require a same-origin check /
   token, so a proxy that adds authentication is not bypassable via cross-site
   requests from a browser on the LAN.

**Alternatives.** Assume root-path hosting only (breaks subpath users). Ignore CSRF
(risky once a proxy adds auth, since the browser carries that auth automatically).

**Consequences.** (+) works behind any proxy, subpath included, without surprises;
(−) a little extra plumbing for base path and CSRF tokens.

## ADR-016: Authentication — delegated to a reverse proxy

**Context.** The owner runs Nginx and does not envision the app needing user accounts
or access control of its own.

**Decision.** The app ships **no authentication and no user management**. Access
control is delegated to the operator's reverse proxy. CSRF protection (`ADR-015`)
remains, so a proxy that adds authentication cannot be bypassed by cross-site
requests from a browser on the LAN.

**Alternatives.** Built-in single-user basic auth (deferred — unnecessary surface
area for the owner's use); multi-user accounts/roles (rejected — out of scope,
`SCOPE.md` non-goals).

**Consequences.** (+) lean; no credential storage, no user database, no session
handling. (−) exposing the port directly without a proxy is unsafe — stated loudly in
`DEPLOYMENT.md`.

## ADR-017: Deferring acknowledgement, and storing maintenance windows as a setting

**Context.** Three Phase 2 items were reviewed together: alert acknowledgement
(`FEATURES.md` B7), maintenance/silence windows (B8), and SMART self-test triggering
(A19). The owner wants all three eventually; the question was which to build now.

**Decision.**

1. **Maintenance windows — built, as a setting.** A window is a JSON list in the
   `maintenance_windows` setting, evaluated by the dispatcher at dispatch time.
   No table, no endpoints, no scheduler involvement. The dispatcher already resolved
   the settings table once per pass, so the marginal cost was a parse and a comparison
   per event. Because nothing sleeps until a window opens or closes, this preserves
   the zero-idle-cost invariant (`FEATURES.md` §E) more cheaply than the
   next-boundary-sleep design originally sketched in `NOTIFICATIONS.md`.
2. **Acknowledgement — deferred, documented as wanted.** It needs a table with expiry
   and "has the condition changed since?" bookkeeping, plus UI to list and revoke
   acknowledgements. At single-host scale that state costs more to maintain than the
   alert noise it removes. The part that would have been expensive to retrofit — a
   stable `event_key` per alert, already recorded in `notification_log` — is already
   in place.
3. **SMART self-test — deferred, and last.** It is the only feature that writes to a
   drive, so it lands after Docker device access has been validated on real hardware —
   which it now has (native NVMe, USB-attached NVMe, USB-attached SATA SSD), so this is
   unblocked and waiting only on being built.
   Two constraints are recorded in `FEATURES.md` A19: it is **opt-in and never
   automatic**, and a running test must show a **progress reading**, because a long
   test on a 14 TB drive runs for many hours and a spinner with no percentage is
   indistinguishable from a hang.
4. **`scan_exclude_devices` — dropped.** A per-device hide flag already covers the use
   case, and two ways to hide a drive is one too many.

**Alternatives.**
- *Build all three now* — the self-test would ship untested against real device
  plumbing, which is the one place a mistake is expensive.
- *A `maintenance_windows` table with CRUD endpoints* — more code, and it would have
  needed the scheduler to wake at each window boundary, adding an idle-cost surface
  for no benefit. Rejected once the dispatch-time check proved sufficient.

**Consequences.** (+) planned maintenance is silenceable today with no new idle cost;
the deferred items are recorded with their constraints so they are picked up, not
rediscovered. (−) acknowledgement stays on the backlog, and the self-test's progress
requirement now constrains its eventual API shape (`API.md`).

## ADR-018: The NAS fleet tier — what a fleet changes

**Context.** The MVP is sized for a workstation with a handful of drives. The owner also
runs a NAS, and a review of the two target use cases — a NAS fleet, and a drive-testing
bench — produced a set of additions that are not "more features" so much as a different
*shape* of problem. The fleet case needed decisions, not just a list.

**Decision.** Build a **Phase 2.5** tier, recorded in `SCOPE.md` and catalogued in
`FEATURES.md`, on these judgements:

1. **A fleet wants a list, not a grid** (`D8`). The card grid stays for small hosts;
   large hosts get a sortable table ordered by risk, with the existing filter/sort work
   (`D5`) feeding it.
2. **Grouping by tag is the organising idea** (`D9`), not a filter. It is what turns
   twenty-four cards into three groups, and it is the precondition for correlated-change
   detection.
3. **A drive gets a physical address** (`D10`) — a bay/slot field, shown on the card and in
   the report. Identity was already solved (serial number, persisted across replug);
   *location* was not, and location is what you need at 3am.
4. **Labels must survive a host migration** (`C7`, promoted from P3). Names, notes and tags
   live in the SQLite volume, which survives a restart but not a move. At fleet scale,
   re-entering sixteen drives by hand is the real cost, so import/export stops being a P3
   nicety.
5. **The projection window follows retention** (`A26`). Retention is configurable and the
   window is not, so an owner who keeps five years of history to watch a slow trend has it
   discarded at 180 days. `API.md` documents the window as a deliberate match to the
   *default* `retention_days`, already bounded by available history — so the one remaining
   asymmetry is the upper bound. This resolves it in favour of the user's own setting.
6. **Correlated-change detection is the fleet-only insight** (`A25`), and deliberately P3.
   Eight drives in one enclosure share a temperature, so a failing fan reads as eight
   unrelated alerts — exactly the noise that teaches people to ignore alerts. It needs
   grouping and history first, and it needs a high bar, because a RAID rebuild legitimately
   heats everything: a *false* "your enclosure is overheating" alert is worse than eight
   true per-drive ones. **Built**, and the high bar turned out to be an admission rather than
   a threshold: the app **cannot tell a failing fan from a RAID rebuild, or from a warm
   afternoon**, and nothing observable from a temperature separates them. So it does not try.
   It reports a **correlation and never a cause**, naming the group and the shared rise and
   leaving the diagnosis to the reader who knows whether a rebuild is running. The two
   thresholds the owner asked for are real — how many drives must move, and by how much — but
   they set how *much* evidence is enough, not how confident the app pretends to be. The
   notification is opt-in per channel for the same reason, the signal is temperature alone
   because that is what an enclosure shares, and the per-drive alerts are deliberately **not**
   suppressed: each one is true, and what `A25` removes is the confusion rather than the
   messages.
7. **"Replace first" is the ranking a fleet asks for** (`A24`), combining verdict, direction
   (`A14`) and remaining life (`A16`), with the reasoning shown. It is the action-shaped
   form of work the app already does.
8. **Evidence is a first-class output** (`A23`/`A27`). The report export already exists;
   saying on it that it is warranty/RMA evidence costs almost nothing, and it is the whole
   reason some people run a scan at all. **Built**, and the sentence turned out to be the
   cheap half. A report that says `BAD` and names no reason is not evidence a support
   engineer can check, so every format now names the attributes that fail the drive's
   **own manufacturer threshold** — the vendor's limit, in the drive's own terms — and the
   counters the app reads, taken from the list the verdict itself uses. Selecting by the
   drive's threshold rather than ours is what stops the report contradicting itself: it is
   a listing rather than a verdict, so a threshold the user changed since the scan cannot
   make the two disagree. The fleet export (`A23`) is the same idea applied to a whole
   rack: one row per drive, from the same rows and the same ranking the dashboard shows, so
   the file and the screen cannot disagree either. Its two rules are worth recording. **A
   missing reading is an empty cell, never a zero** — the em-dash rule from the UI, and it
   has teeth here because this is the file the numbers get aggregated in: a blank is what a
   spreadsheet's `AVERAGE` skips, while a `0` drags every average down and reads as a
   measurement. And **the export is never filtered**: a wrong number gets noticed and a
   missing row does not, so `hidden` and `connected` are columns rather than filters.

**Alternatives.**
- *Ship a Grafana/InfluxDB-shaped graph stack* — the approach Scrutiny takes. Rejected: it
  is a second service to run, and it answers "what did this drive do" rather than "what
  should I do about it".
- *Make the fleet view the only view* — rejected. Four drives on a workstation is the other
  half of the audience, and a table is worse for it.
- *Build correlated detection first, since it is the differentiator* — rejected. It is the
  one item here that can produce a **wrong** alert, and it depends on grouping and history
  that do not exist yet.
- *Add grouping as a filter on the existing grid* — rejected. Grouping is structural: it
  changes the layout and feeds `A25`, so a filter would have to be redone.

**Consequences.** (+) the app scales to a fleet without abandoning the workstation case;
every item is a view over data already stored, so the zero-idle-cost invariant
(`FEATURES.md` §E) is untouched. (±) `A26` changes existing behaviour — the window stops
being a constant and starts tracking a setting, which the longevity tests must cover.
(−) `A25` carries genuine false-positive risk and will need a deliberately conservative
threshold plus a way to see *why* it fired.

## ADR-019: Working order — documentation, then implementation, then UI polish

**Context.** Feature ideas were arriving faster than they were being built, and the project
has a stated character ("sleek and minimal") that a UI growing feature-by-feature will
erode. Ideas were also at risk of being forgotten or re-litigated between sessions.

**Decision.** Every batch of work runs in three moves:

1. **Documentation first.** The idea is written into `FEATURES.md` (and `SCOPE.md`, plus an
   ADR when it carries a real trade-off) *before* any code. The catalogue is the memory.
2. **Implementation**, with the test discipline the rest of the project already carries.
3. **UI spot-cleaning.** After the batch, the interface is reviewed as a whole — grouping,
   hierarchy, what to demote into a secondary surface — and the outcome recorded in
   `UI.md`. This is a pass, not a feature.

**Alternatives.** *Document as you go* — how the project drifted; it produced some
catalogued items and some uncatalogued ones, and the uncatalogued ones were the ones that
got lost. *Clean the UI only when it feels bad* — reactive, and it makes the cleanup a
rewrite rather than a pass.

**Consequences.** (+) ideas survive between sessions, and the UI never accumulates a batch
of unreviewed controls. (−) one extra step before code and a deliberate pause after each
batch — which is the point.

**Note (status, not a rewrite).** `ADR-017` deferred the S.M.A.R.T. self-test "and last".
That ordering is now overtaken: device plumbing is validated on real hardware, so the
self-test is the next build item after the read-only work, and `SCOPE.md` Phase 2 says so.
`ADR-017`'s constraints on it — opt-in, never automatic, and it must show a progress
reading — are unchanged and still binding.

## ADR-020: The runtime image is Alpine, not distroless

**Context.** `ADR-001` recorded "distroless image <20 MB" as a consequence of choosing Go,
`DESIGN.md` repeated it as a build target, and the README turned it into a feature bullet.
The image has in fact always been an `alpine` base (`Dockerfile`), because the runtime needs
`smartctl` and `lsblk` and both are packaged there. Nothing ever reconciled the two, so for
the life of the project the documentation described an image that was not the one being
built — found while preparing for a public release, which is exactly when it would have
started to matter.

**Decision.** The runtime image is **`alpine`**, with `smartmontools` and `util-linux`
installed from apk. `ADR-001`'s actual decision — one static Go binary, frontend embedded —
is unchanged. Only its claim about the base image is corrected, and the stale size figure is
**withdrawn rather than replaced with another estimate**, because an unverified number is
what caused this in the first place.

**Alternatives.**
- *`distroless/static` with a statically-linked `smartctl` copied in* — smaller, and a smaller
  attack surface. Rejected: `smartctl`'s vendor quirk database is the product's core
  (`ADR-002`), so it is worth taking the maintained Alpine package rather than building,
  pinning and updating a static binary ourselves. `lsblk` comes along with it.
- *Keep the distroless claim and change the Dockerfile to match it* — rejected as the wrong
  direction: it would trade a maintained dependency for a self-built one in order to satisfy a
  sentence written before the tooling had been chosen.

**Consequences.** (+) the documented image is the built image, and the runtime tools are
updated by apk rather than by us. (−) the image is larger than the distroless target `ADR-001`
imagined, and the runtime libc is musl rather than glibc. (±) no size is stated anywhere until
it is measured and published; when it is, it belongs in the README and `DEPLOYMENT.md`
together. `ADR-003`'s passing mention of "static distroless builds" is about why the pure-Go
SQLite driver was chosen and is unaffected — pure-Go still means a static binary either way.

**Measured (v1.0.0).** `docker image ls` on a homelab host reports **14.8 MB content size**
and **53.5 MB disk usage** — so the withdrawn claim of "well under 20 MB" was true of the
compressed figure and wrong about everything else. That is precisely why the two numbers are
now published together and labelled: a single figure is what let the original claim survive
unchallenged for the life of the project.

Both figures were measured on the `alpine:3.20` image v1.0.0 shipped. Dependabot has since
moved the base to 3.24, so a re-measure belongs with the next release (`RELEASE.md`) — which is
also the reason the version is no longer written into prose anywhere.

## ADR-021: Notification secrets are database-only, and the docs now say so

**Context.** `ADR-013` decided that notification secrets would come from three sources with
precedence **environment > mounted secret file > database**, and that "the UI displays each
secret's source". `DEPLOYMENT.md` documented all three, including a worked
`SMARTSERVE_NTFY_TOKEN_FILE=/run/secrets/ntfy_token` example, and told the reader to prefer
the env or file source when managing configuration as code.

None of that was ever built. Channel secrets are read only from the channel's stored config
in SQLite; the app's only environment reads are the six operational `SMARTSERVE_*` variables
in `internal/config/config.go`. There is no `*_FILE` handling anywhere, no env override for a
channel secret, and no secret-source display in the UI.

This is the most damaging kind of documentation defect: a reader who follows it gets no error
at all. The variable is simply ignored, the channel has no token, and notifications fail
silently at the moment they were supposed to matter.

**Decision.** For v1, the documentation is corrected to describe what exists: notification
secrets are set **per channel in the web UI** and stored in the database.
`DEPLOYMENT.md` §Secrets says so, and states plainly that setting a `*_FILE` variable today
does nothing. `ADR-013`'s decision is not withdrawn — it is **deferred, and recorded as
wanted**, which is the same treatment `B7` (alert acknowledgement) received in `ADR-017`.
Environment and `*_FILE` resolution is a v1.1 candidate, and the shape is already decided.

**Alternatives.**
- *Implement `ADR-013` before v1* — the most complete answer, and the one that makes the
  original document true. Rejected for now on scope: it is a security feature with a UI
  surface (showing each secret's effective source) and its own tests, and v1 is the MVP plus
  corrections rather than new features. It is the first thing to build afterwards.
- *Leave the docs alone and implement it later* — rejected outright. The gap is not that a
  feature is missing; it is that the docs assert it exists, so a reader is actively misled.
- *Quietly delete the section* — rejected: a reader who found the old text in a cached copy
  deserves to learn it was wrong, and the deferral is a real commitment worth recording.

**Consequences.** (+) the documentation is true, and the gap is recorded rather than
rediscovered. (−) secrets live in `/data/smartserve.db`, and therefore in whatever that
volume is backed up with — which is precisely the outcome `ADR-013` was written to avoid, so
`DEPLOYMENT.md` now says so where a reader will see it. (±) implementing `ADR-013` later
supersedes this ADR rather than contradicting it.

**Superseded by `ADR-022`.** The deferral lasted one session: the mechanism was built for v1
rather than shipped as a documented absence, and the per-channel form was chosen over the
global variables this ADR left open. What remains true here is the finding — that a
documentation defect which produces no error is worse than a missing feature.

## ADR-023: Committing to the rest of the roadmap, and the decisions that unblock it

**Context.** With v1.0.1 public and the first round of real-use fixes out, the deferred work was
reviewed in full. The owner committed to **all** of it — the large features (`A19`, `D8`, `D9`,
`D10`, `A21`, `A25`), the medium ones (`A20` entries, `A18`, `A17`, `B7`, `C7`, `A24`, `A26`) and
the small ones (`B9`, `B10`, `A23`, `A27`, `D11`, `D5`) — plus `C6` (Prometheus) and a rework of
the trend section. Nothing was tabled.

That is a lot of surface to hold in a conversation, so the commitment and its order are recorded
here rather than remembered.

**Decision — the order.** Each round is a shipping unit, and a UI consolidation pass follows each
(`ADR-019`).

1. **`A20` entries, then `A19` self-test.** They are the same data: the self-test log *is* the
   result record. Everything `A19` needs already exists — validated device plumbing, the drive's
   own `polling_minutes` as the progress basis, a captured self-test log, and
   `self_test_supported` to disable the control. `B9` (self-test result notifications) rides on
   it.
2. **`D5` → `D8` → `D9` → `D10`.** The fleet tier's foundation, in that order: `D8` depends on
   `D5`, and `D9` is the precondition for `A25`.
3. **`A18` + `A17`.** The insight pair, both over history that is already stored.
4. **`C7` + `A26` + `A24`.** The rest of the fleet tier. The two evidence items (`A23`, `A27`)
   went with it: they are the same surface — the report `A24` already reads, and the rows the
   fleet table already shows — so building them beside it cost nothing extra and kept one set
   of decisions instead of two.
5. **`A21` knowledge base.** Needs data collection rather than code, so it starts whenever the
   owner wants a break from implementation.
6. **Notifications polish:** `B7`, `B10`, `B13`.
7. **`C6` Prometheus** and the trend-section rework, fitted in where they suit.

**Decision — the five questions the owner left open.**

1. **`A19` surface: the button first, scheduling second.** A user-created schedule is not the same
   thing as an automatic test. `A19`'s constraint is that nothing fires a self-test because the
   app started, a scan ran, or a drive appeared, and a schedule the user wrote satisfies that. So
   both are wanted. The button ships first because it is the primitive, and because scheduling
   carries a problem the button does not: **a fleet**. Twenty-four long tests starting together is
   an I/O meltdown, so a scheduled self-test must run **one drive at a time**, in a defined order.
   That is worth designing deliberately rather than bolting on, and the default must be *short* —
   a long test on a 14 TB drive runs for hours.
2. **`A25` threshold: configurable, with a documented default.** The owner's fleet is four to six
   drives; other people run twenty-four. A fixed number would be wrong for one of them, so the
   grouping size and the temperature delta that counts as "moving together" become settings, with
   defaults chosen for a small enclosure and a note about what raising them means.
3. **`A21` data: community pull requests.** The curated layer needs rated TBW per model family,
   which is published data but not machine-readable. The owner's drives are Western Digital,
   Seagate and Samsung consumer/prosumer parts, which seeds the first entries and gives
   contributors a shape to copy.
4. **`D7` localization: deferred, and this ADR is where that is decided rather than assumed.**
   See below.
5. **`C6` Prometheus: build it, off by default.** The owner does not run Prometheus but recognises
   that plenty do. A scrape endpoint does not violate the zero-idle-cost invariant, since nothing
   on this side polls; it is still opt-in, because enabling it invites someone else's scraper to
   poll this app, and that is the owner's decision rather than a default.

**`D7` — why localization is deferred.** `D7` is translating the interface into other languages.
CrystalDiskInfo is popular partly because it is translated, so the case is real. It is deferred for
one reason: the expensive part is not the translations, it is **extracting every string** from the
interface into a locale file, and then keeping every locale in step as features land. That refactor
touches every file in the UI, and a single maintainer who cannot review a translation cannot
maintain one either. Recording it as *deferred* rather than *dropped* means the mechanism can be
built when there is a translator who wants it — the work is only worth doing for someone who will
maintain the result.

**The trend-section rework, decided in the same breath.** The owner asked for three things, and
they turn out to be one change: the x-axis becomes **time rather than reading index**. That single
change gives all three — a 30-day window spans 30 days whether or not anything was recorded, a
series that began five days ago starts a sixth of the way across instead of at the left edge, and a
date axis under the card can show the span without hovering. Per-metric y-scaling stays automatic.
One shared date axis under the card is preferred to a label under each sparkline: four sparklines
share one window, so they need one axis, and four would be noise.

**Alternatives.**
- *Pick a subset and ship it sooner* — rejected. The owner reviewed each item and wants all of
  them, and the order above already front-loads the two with the most value.
- *Schedule self-tests through the scan scheduler* — rejected for now. Scans take seconds and
  self-tests take hours, and the staggering requirement means the two want different machinery.
- *Hard-code `A25`'s threshold* — rejected: the fleet size it has to suit spans an order of
  magnitude.
- *Do `D7` now with machine translation* — rejected. An unreviewed translation is worse than
  English, because a wrong word in a health warning is a wrong warning.

**Consequences.** (+) nothing on the roadmap is ambiguous about whether it is wanted, and the order
is fixed, so a future session resumes rather than re-decides. (−) the project is committed to a long
tail of work, and `D7` is the one item deliberately left out. (±) the trend rework touches
`sparkLine`, which is pure and well tested — the change is contained, but its tests assert
index-based coordinates and have to be rewritten alongside it.

## ADR-022: Per-channel environment overrides, keyed by the channel label

**Context.** `ADR-021` corrected the documentation to admit that `ADR-013`'s three secret
sources had never been built, and deferred the work. The owner chose to build it for v1 rather
than ship with the gap, and specifically chose the per-channel form over a global
`SMARTSERVE_NTFY_TOKEN`-style variable.

That choice follows from the reason `ADR-013` put secrets in the database at all: a secret
belongs to a **channel**, not to the process. Two ntfy channels — a noisy one and a quiet one —
need two tokens, and a variable named after the channel *type* cannot express that. It is also
a trap rather than a simplification: it works for exactly one channel of each type, and the
migration to something that works for more is the same work later.

**Decision.** Every string field of every channel is overridable from the environment, named
after the channel's **label**:

```
SMARTSERVE_CHANNEL_<LABEL>_<FIELD>            # the value
SMARTSERVE_CHANNEL_<LABEL>_<FIELD>_FILE       # a file to read the value from
```

where `<LABEL>` and `<FIELD>` are uppercased with each run of non-alphanumerics reduced to a
single underscore. A channel labelled `Critical alerts` with a `token` field therefore reads
`SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN`. Precedence stays **environment > file > database**,
as `ADR-013` decided.

Four judgements sit inside that:

1. **Keyed by label, not by id.** An id is assigned on insert, differs between a fresh database
   and a restored one, and changes when a channel is recreated — none of which a compose file
   can know. A label is what the operator writes. The cost is that renaming a channel changes
   its variable names, which is why the editor displays the name and recomputes it as the label
   is typed: a rename is then *visible* rather than silently falling back to the database.
2. **Every string field, not only the ones marked secret.** One mechanism then covers both
   "keep the bot token in a Docker secret" and "point this channel at a different ntfy server
   per environment", and there is no second list of secret field names to keep in step with the
   adapters as they change.
3. **A broken `_FILE` is an error, never a fallback.** A missing or empty mounted secret reports
   itself and the delivery is recorded as failed. Falling back to the stored value would send a
   stale token while the operator believed they had rotated it — the same silent failure that
   made the undocumented version of this feature so damaging.
4. **The API returns provenance, never values.** The editor needs to know *that* a field is
   supplied, not what it holds. Handing a resolved secret to the browser would leak an
   environment secret into the page, and the next Save would write it into the database —
   quietly promoting a secret that was meant to stay out of it. The server reports the *names*
   of the variables it has set and the editor derives its badges from them; those names come
   from labels the operator chose, so nothing is disclosed that the interface did not already
   show.

**Alternatives.**
- *`SMARTSERVE_NTFY_TOKEN`-style globals* — rejected, per the context above.
- *Keying by channel id* — rejected: not knowable from a compose file, and unstable across a
  restore.
- *`*_FILE` for secrets only, leaving other fields alone* — rejected as two mechanisms for one
  idea, and as a list that would need maintaining alongside the adapters.
- *Returning resolved values so the form shows what will be sent* — rejected: it leaks the
  secret and then writes it back.

**Consequences.** (+) a token can live in a Docker secret, so backups of `/data` stop carrying
it; the same mechanism configures a channel entirely from the environment; and a mismatch
between a label and its environment is visible in the editor rather than silent. (−) the naming
rule now exists twice — in `internal/notify/secrets.go` and in
`internal/webui/assets/js/views/notifications.js` — because the name has to appear while the
label is being typed, before anything is saved. Both are pinned by tests using the same table of
cases, which is the only thing keeping two implementations of one rule honest. (±) `ADR-021` is
superseded, not contradicted: it remains the record of how the gap was found and why the
documentation was corrected in the meantime.





