# Smart-Serve feature registry

Smart-Serve monitors the S.M.A.R.T. health of the drives in one machine: one Go binary with
embedded assets, SQLite for history, no external services. Tiers follow `SCOPE.md`. Every feature
must preserve the zero-idle-cost invariant (`DESIGN.md` §2.1): nothing polls a device or runs a
timer while the app is idle.

Legend: 💤 no idle cost · 🔌 active only during a scan · ⚠ needs an opt-in guard. `done` in Notes
marks a feature completed after this catalog began; a blank Notes cell means it shipped before the
column existed, not that it is missing.

The interface has five pages: Fleet (`/`), Device (`/device/<serial>`), Schedules, Notifications and
Settings (Thresholds, Notifications, Appearance, Data). The card grid, the separate dashboard,
`dashboard_view`, `show_replace_first`, the replace-first shortlist card, `?ui=`/`data-ui`, the
testing-session card and the "remind me later" snooze are retired; they are not features and are not
described below. The ranking the shortlist used survives as the digest's drive order
(`internal/notify/digest_sender.go`).

Some long-standing facts are served by the API but are not drawn by the rebuilt pages: notes, tags and
bay (`PUT /api/devices/{serial}/notes` and `GET /api/devices/{serial}`), acknowledgement
(`PUT /api/devices/{serial}/ack`), the `-d` transport override, the score breakdown, the longevity
projection, knowledge ratings and advisories, and the "since the last scan" diff. Their entries say
which surface carries them.

## What is left

| What | Where it is recorded | Why it is open |
|---|---|---|
| `D7` localization | `SCOPE.md`, `ADR-023` | Deferred, not dropped: extracting every string and keeping every locale in step is only worth doing for a translator who maintains it |
| Error-log entry lists (`A20`) | §A20 | ATA entries render only when they carry detail; the one populated log captured is a fixture, and no drive here has ever logged a real error |
| arm64 on real hardware | [TESTING.md Pass 8](TESTING.md#pass-8--arm64-raspberry-pi), `SCOPE.md`, `RELEASE.md` | Native arm64, startup health, a container SMART read over USB SAT and identity checks passed on a Pi 5 / DietPi (2026-11-08); two defects found there were fixed and retested on arm64 and amd64. Idle memory cannot be read on that host (`docker stats` reports `0B / 0B`); CPU is near zero and amd64 measures 18.9 MiB idle. Overnight stability and broader parity remain open |
| Multi-user accounts and roles (`D23`) | `SCOPE.md` Phase 3, `ADR-016` | Not planned: more than one user is a reverse proxy's job. The single-user gate (`C5`) adds no accounts, roles or migration |

Everything else recorded in these documents is either shipped or an explicit non-goal: writing to
drives, RAID-controller passthrough, udev hot-plug detection, host CPU/RAM metrics, and multi-user
accounts with roles (`SCOPE.md` §Explicit non-goals).

## A. Core reporting (the S.M.A.R.T. mission)

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| A1 | Device list with `lsblk` parity (internal + USB) | MVP | 🔌 | `lsblk` is the source of truth; a drive smartctl cannot read still appears |
| A2 | On-demand Scan with SSE notification | MVP | 🔌 | `Scan now` in the fleet header and the sidebar; `scan_started` and `scan_complete` arrive over SSE and the fleet redraws when the scan finishes |
| A3 | Device detail: full attribute table (value/worst/threshold/raw) | MVP | 💤 | CrystalDiskInfo layout. Per-model thresholds override the global defaults; see §A3 |
| A4 | Overall verdict with plain-language rationale | MVP | 🔌 | Healthy / Caution / Failing / No reading, with the reason that produced it |
| A5 | Temperature, power-on hours, power cycles, capacity | MVP | 🔌 | Power cycles are in the NVMe health log and in ATA attribute 12 |
| A6 | Firmware, serial, WWN, interface speed | MVP | 💤 | Firmware and serial head the device page; WWN and interface speed are stored and served by the API, not drawn |
| A7 | Drive age | MVP | 💤 | Power-on hours are shown in the fleet row and the device facts; no derived "N years in service" string is rendered |
| A8 | Copy / export a S.M.A.R.T. report | MVP | 💤 | Four formats (`txt`, `html`, `csv`, `json`) via `?format=`; the device page links the HTML one directly |
| A9 | Fleet glance (n healthy / caution / failing / no reading) | MVP | 💤 | One line under the Fleet title; each count is a filter, clicking it again clears, and the line carries the time of the last scan |
| A10 | Raw `smartctl` output viewer | MVP | 💤 | "Show raw output" on the device page; vital for USB bridge quirks |
| A11 | Per-device transport override (`-d sat`, ...) | MVP | 💤 | Stored per drive and used for every later read; set through `PUT /api/devices/{serial}/transport` |
| A12 | Attribute help and score breakdown | MVP | 💤 | A curated `?` per interpreted attribute and NVMe counter, plus the stored penalty list; see §A12 |
| A13 | Scan history | MVP | 💤 | `GET /api/scans`: start and end, trigger, success, per-device counts and error |
| A14 | Degradation trend (worsening flag) | P2 | 💤 | Bad-sector growth over the retention window; feeds the score penalty and the digest order; see §A14-A17 |
| A15 | History charts (temp, sectors, wear) and sparklines | P2 | 💤 | Six sparkline cells on the device page with a 7/30/90-day/all range picker; x is time, with one shared date axis |
| A16 | Remaining-longevity estimate | P2 | 💤 | NVMe wear rate, ATA sector fallback, confidence labels; see §A14-A17 |
| A17 | Health score 0-100 | MVP/P2 | 💤 | Trend-aware, capped penalty, no letter grade; see §A14-A17 |
| A18 | Snapshot diff ("what changed since last scan") | P2 | 💤 | Server-decided findings; see §A18 |
| A19 | SMART self-test (manual and scheduled) | P2 | 💤 | Short/long, live progress, abort, `selftest` schedule action; see §A19 |
| A20 | SMART error and self-test logs | P2 | 💤 | Counts, entry tables when entries carry detail, the drive's own timings, smartctl's message when a log cannot be read; see §A20 |
| A21 | Drive knowledge base (rated TBW, firmware advisories) | P3 | 💤 | Bundled table, optional URL refresh, your own entries; see §A21 |
| A22 | Drives with no S.M.A.R.T. (USB flash, card readers) | P2 | 💤 | A plain notice plus the facts `lsblk` has; see §A22 |
| A23 | Fleet-wide export (every drive in one CSV or page) | P2 | 💤 | One row per drive; a missing reading is an empty cell, never a zero |
| A24 | Replace-first ranking | P2 | 💤 | Bands with reasons; orders the digest; the fleet card is retired; see §A23, A24, A27 |
| A25 | Correlated change (N drives in a group moving together) | P3 | 💤 | Temperature only, opt-in notification, correlation never cause; see §A25 |
| A26 | Projection window follows retention | P2 | 💤 | The projection, the worsening flag and the trend adjustment all use `retention_days` |
| A27 | Warranty / RMA evidence pack | P2 | 💤 | Every export names its purpose and the evidence; see §A23, A24, A27 |
| A28 | Partition schema and storage usage | P2 | 💤 | The `lsblk` tree kept instead of flattened; see §A28 |

### A3: per-model thresholds

Thresholds are a table on the **Thresholds** settings page: one row per model in the fleet, one
column per threshold, and a blank cell inherits the global value (shown as a placeholder). The
common edit is one number for one model, so the editor seeds a row per model at the current values
and merges rather than replaces. Stored as `model_thresholds` and validated for both shape and
setting names on save. It moves the **verdict only**: the report's evidence still lists attributes
failing the drive's *own* manufacturer threshold (`A27`), so the two cannot contradict each other.

### A12: attribute help and the score breakdown

The attribute table is the densest thing the app shows. Each row the interpreter acts on carries a
`?` button (not a `title`, because hover-only help is unusable on mobile) explaining the field and how
to read it; the list is curated to the interpreter's attributes and a test keeps the two from
drifting. One judgement is made in the UI, and only one: a row is marked when the **drive itself**
reports the attribute at or below its own threshold, and a threshold of 0 means no threshold, so
informational rows are never flagged. The score breakdown (`interp.Penalty`) is stored in
`devices.penalties` (migration 0014) and served as `penalties`. It carries the reasons and the points
each cost, including the trend penalty, and it marks the attribute rows the verdict read, so "at
threshold" (the drive's alarm) and "counted" (this app's score) stay distinct claims. The rebuilt
device page shows the score, not the breakdown.

### A14, A16, A17: direction, longevity, score

All three read the same history and are deliberately modest: a S.M.A.R.T. reading does not know the
future.

- **A14, the worsening flag.** Bad sectors accumulating over the retention window, served as
  `worsening` on the device payload. It is separate from the verdict: the verdict says how the drive
  looks now, the flag says which way it is going. It drives the score penalty and the digest order.
  Neither page draws a worsening badge today.
- **A16, longevity.** Extrapolated from NVMe `percentage_used`, reported as a horizon with a
  confidence (`low` under a month of history, `medium` beyond) and its caveat inline. A window
  shorter than a day produces no rate at all. Sector growth is reported as a rate and a window
  ("7 new sector problems in 60 days"), never a date, and ATA wear counters are not extrapolated:
  an ATA drive falls back to the sector rate. Served as `projection`; not drawn.
- **A17, the score.** 0-100. A drive whose bad sectors climb loses 3 points for the first new
  sector, then one more per doubling, capped at 10. The penalty is applied in the scan, outside
  `interp.Evaluate`, because a trend is a question about history and `Evaluate` is a pure function
  of the drive's own data; it is skipped for a drive that could not be read. The cap is small
  enough that the trend alone can never carry a drive out of its verdict. No letter grade is
  displayed.

### A18: what changed since the last scan

Served as `changes` on the device detail payload. Three rules: "nothing changed" is a real answer
and is said in words; an absent reading is not zero (it renders `n/a`, never 0 °C); and the
**server** decides what counts as a finding, so this cannot disagree with the verdict. An attribute
movement is a finding if it meets the drive's own threshold or is one of the lost-storage counters
(5, 197, 198); everything else is counted ("4 other counters moved, which is ordinary wear") rather
than silently dropped, and a counter the drive has just started reporting is marked new. The card
component exists; the rebuilt device page does not mount it.

### A19: self-test

Manual: a short or long button, live progress while a test runs, and an abort behind a confirmation.
This is the only feature that writes to a drive, so it is opt-in and never automatic: nothing fires
a test because the app started, a scan ran or a drive appeared. The API enforces it further with an
allow-list of two test kinds, so nothing else can reach `smartctl -t`.

Progress comes from the drive's own figure: the ATA self-test log's `remaining_percent`, or the NVMe
self-test log's header percentage, which is a separate field rather than a table row. The drive's
own `ata_smart_data.self_test.status` outvotes a leftover `remaining_percent` in both directions, so
an interrupted test cannot report itself busy forever; absent status means silence rather than idle.
Both logs are parsed; a parser that knew only the ATA shape reported "no test running" for a drive
that was mid-test. A drive reporting no percentage is never rendered at 0%, and a drive whose log
cannot be read (`self_test_log_unread`) is told so and still offered the buttons. That notice is
raised only when the drive offered **no** self-test log this app reads, so a key like
`ata_smart_selective_self_test_log` does not trigger it.

A finished test triggers one rescan, guarded per card, because the redrawn card would otherwise see
the old stored log still showing the test running and poll forever. A test the card started counts
even if the card never saw it running (a short test can finish between polls), and the card shows
the last finished result itself. Scheduling rides the schedule table as `action: selftest` with
`selftest_kind` short or long, not a second timer (`ADR-024`). A pass runs one drive at a time in
serial order, because 24 simultaneous long tests is an I/O meltdown. While a pass drains, the
Schedules page shows how many drives are done and which one is testing.

A server-side watcher re-reads **only drives already mid-test**, on `selftest_poll_seconds`
(default 15, range 2-30), and publishes `selftest_progress` over SSE so a badge does not freeze at
whatever the last scan caught. It cannot wake an idle drive, it writes three columns rather than a
whole row so it does not fight a scan, and it stops when the last test ends.

### A20: the drive's own logs

The question is "has this drive ever complained?", and the answer is mostly a count. The Drive logs
section shows how many entries the error and self-test logs hold, the drive's own estimate of test
duration (for example `short ≈ 2 min · extended ≈ 133 min`), whether it supports self-tests, and
smartctl's words when a log cannot be read. A count of zero is shown, because "this drive has never
complained" is the answer somebody came for. Entry rendering is shape-dependent. `-x` reads the ATA
extended error log; the summary log is often reported as supported while permanently empty. On a
drive smartctl does not recognise, an error entry carries an error number and an index and nothing
else, so the log renders a table only when entries carry something beyond their identity; otherwise
the count stands alone with a sentence saying the drive reported no detail. NVMe has no equivalent:
the health log carries `num_err_log_entries` and the Error Information log prints no entries, so the
count is all there is. The self-test log is a different shape (`type`, `status` including
`remaining_percent`, `lifetime_hours`) and is rendered in full; it is the record `A19` writes. A test
in progress is a third state, distinct from passed and failed, because an absent verdict is not a
failure. The summary is parsed on read from the stored raw document, so there is no migration and no
snapshot column; it is omitted from the device list so a fleet refresh does not parse every drive's
document. Only one populated ATA error log has ever been captured, and it is a fixture
(`ata-bto-1tb-logs.json`); the populated shape has not been seen on real hardware.

### A21: knowledge base

Three layers, later winning: a table bundled with the build, a copy fetched from a configurable
`knowledge_url` (defaulting to the published file), and your own entries typed in Settings. The
bundled and refreshable files share one format, one validator and one schema version, so the data
can be maintained by somebody who did not write the app. The refresh is a button, never a timer, and
the card shows how old the stored copy is.

Three rules keep it from inventing a number:

- **Endurance is per capacity.** Entries are keyed by the decimal GB the drive reports; within 10%
  counts as the bucket and within 2% is exact, so the app can say "600 TBW" rather than "about".
  A capacity with no close bucket gets no figure, never a neighbour's.
- **Matching is a substring on model or family**, longest pattern wins, and patterns under five
  characters are refused. `matches` covers a series such as `WD20EFAX` through `WD80EFAX`.
- **Advisories only appear on a firmware match**, because a drive with no firmware cannot match a
  firmware-specific advisory. An advisory with no firmware is family-wide.

Every figure carries its source, shown beside it. The knowledge base changes no verdict: it supplies
a denominator for wear and an advisory when the firmware matches. The shipped table is deliberately
short (Samsung consumer SATA and NVMe, Crucial MX500, WD Red/Red Plus/Red Pro/Black, Seagate
IronWolf/IronWolf Pro/Barracuda), with one advisory, the SMR Red line. Your overrides travel in the
config export (`C7`); the fetched cache does not. Served as `knowledge` on the device endpoint and
managed on Settings > Data.

### A22: drives with no S.M.A.R.T.

USB flash drives and card readers mostly do not implement S.M.A.R.T., and some bridges do not pass
it through. The device page says so plainly: a neutral message, not an error, explaining that the
identity, capacity, filesystem and mountpoints come from the operating system. `removable`,
`fs_version` and lsblk's `REV` (the firmware fallback) are stored alongside the rest. No synthetic
score, benchmarks or usage graphs are invented. A real SSD or HDD in a USB enclosure usually does
report S.M.A.R.T. and needs the transport override (`A11`).

### A23, A24, A27: exports, ranking and evidence

`A23` offers the whole fleet as a **CSV** (one row per drive, for sorting when planning a purchase)
or a **printable page** (for several drives going back at once), both from one column list and the
ranking's order. A missing reading is an empty cell, never a zero, because this is the file numbers
get aggregated in. The export is **never filtered** by the page's view: `hidden` and `connected` are
columns. Disconnected drives are listed but unranked, with an empty band rather than `fine`. `?tag=`
scopes a batch file, and the first column is Bay.

`A24`: the ranking combines verdict, direction (`A14`) and remaining life (`A16`) into **bands**
(replace now / soon / watch) with the reasons shown, rather than a weighted sum nobody could explain
or argue with. A disconnected drive is excluded: it is a finding of its own, and ranking it would put
a backup drive somebody unplugged above one that is failing. The shortlist card is retired, because
the table already sorts worst-first with the score in every health cell; the ranking remains on
`GET /api/devices` as `ranking` and orders the digest's drives (`ADR-032`).

`A27`: every report closes, at its foot, with its purpose and provenance: the reader opened the file
for the drive. It names the attributes failing the drive's *own* manufacturer threshold and the
counters this app reads as evidence, from the same list the verdict uses. It claims evidence, not
proof: whether that is a warranty case is the vendor's call. Formats are `txt`, `html`, `csv` and
`json` on `GET /api/devices/{serial}/report`.

### A25: correlated heat

Eight drives in one enclosure share a temperature, so a dying fan can present as eight unrelated
alerts. This reports **one** finding about the group instead, and it never claims a cause: the app
cannot tell a failing fan from a RAID rebuild or a warm afternoon, so the wording says only that
enough drives in a group rose together for the cause to be more likely in the enclosure than in the
drives. The signal is temperature only; sector counts and wear are per-drive by nature. The check is
a **rise** between the previous reading and this one, not a level. Two settings:
`correlation_min_drives` (default 4, minimum 2) and `correlation_delta_c` (default 8, must be
positive). A smaller value than those is refused rather than obeyed, so a typo cannot turn the check
into "any one drive that moved". A drive counts for each of its tags, bench drives are excluded
because a drive being tested is expected to heat up, and the per-drive alerts are deliberately not
suppressed: each is true, and silencing true findings is how a monitor becomes untrustworthy. The
result is served as `correlation` on `GET /api/devices`; the notification event `group_heat` is
opt-in per channel, like the scan heartbeat, and the fleet page does not draw it as a card.

### A26: the projection window

Retention is configurable, and the projection, the worsening flag and the trend adjustment all use
`retention_days` itself rather than a fixed 180-day window. What the user keeps is what the app
reads, and there is one number rather than two.

### A28: partitions, usage and the disk map

`lsblk` returns a whole device tree; the app keeps it in `disk_layout` instead of flattening it into
two strings. The rule behind the section: if a fact is already on the wire, the app shows it or says
why it does not.

The device page's **Disk space** section shows a usage line, a map and a table:

- **One row per partition**: label (with the node name beneath), size, filesystem and version,
  partition type under the filesystem, mountpoints, used percentage and a read-only badge. A copy
  UUID button appears where a UUID exists, filesystem UUID first with `PARTUUID` as the fallback for
  a partition with no filesystem. UUIDs are not a column: two long opaque strings per row with
  nothing to scan.
- **Three different empty answers**: nothing mounted is a dash (`not mounted`); mounted but outside
  the container is `not visible` with a tooltip explaining the bind mount; mounted and visible is the
  figure. Conflating the first two would send somebody to edit their compose file for a partition
  with no filesystem. Usage needs a read-only mount root in the container (`- /mnt:/mnt:ro` or
  `- /:/host:ro`); `/dev:/dev:ro` is not enough.
- **A map above the table**, one segment per **top-level** partition, sized by its share of the disk
  and filled from the left by its own fullness. Logical volumes and dm-crypt mappings live inside a
  partition, so drawing them beside their container would claim the disk holds more than it does.
  An unreadable fullness is hatched rather than drawn at 0%, and free space appears only when it is
  a share of the disk, not an alignment remainder. Pointing at a segment marks its row and pointing
  at a row marks its segment, matched by name; a row with no segment (a volume group) marks nothing.

**A drive with no partition table says what it holds.** `whole_disk_fstype` and a plain-language
`whole_disk_name` are kept only when there is no table, so a disk pulled from an md array says it is
"a member of a Linux md array". The same path covers a ZFS pool member, an LVM volume, a LUKS
container and a filesystem on the raw device. On a partitioned disk the node's own signature *is* the
table, so reporting it would claim a "gpt filesystem". **⚠ The array and pool recognition is
unverified against real hardware**: built from blkid's tokens, exercised only against fixtures.
Verified: a partitioned disk reports no whole-disk signature, an unknown signature falls back to the
old message.

**Settings.** `show_partitions` and `show_usage` (both on by default) gate the table and the usage
column; a column is dropped rather than emptied, because an empty column is a worse way to say "you
asked not to see this". `storage_warn_pct` (default 90, range 50-100) turns a usage bar amber and
lives on the Thresholds page. It does not affect the verdict and is not in the per-model seed.

**Storage over time.** `snapshots.used_pct` (migration 0016) records the fullest filesystem's
percentage at every scan, so the Trends strip gains a **Storage** row. It is denormalized out of the
device's partition schema for the same reason `sectors` is: a chart must never load a blob to draw a
line. The delta is reported in points, not per cent. Sector sizes (512e vs 4Kn), TRIM / discard
support, partition table type, unallocated bytes and the long "not visible" explanation live in
`disk_layout` and in the Details disclosure component; the rebuilt page renders the map and table
without that disclosure.

**A stale drive is re-read on open.** If the stored data is older than `device_refresh_minutes`
(default 15, `0` never), opening the page reads that one drive and redraws, and says so. This is the
only thing that touches a drive nobody asked it to touch, which is why it is a setting: a scan
cannot wear a drive, but it can wake one. A drive with no data is read regardless. There is at most
one attempt per window per drive, because `last_scan` advances only on a successful read and an
unreadable drive would otherwise loop forever.

## B. Alerts and notification

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| B1 | ntfy channel (first-class) | MVP | 💤 | |
| B2 | Generic webhook channel | MVP | 💤 | |
| B3 | Health-transition alerts with squelching | MVP | 💤 | `squelch_seconds`, default 3600 |
| B4 | Temperature alerts | MVP | 💤 | |
| B5 | Device appeared / disappeared alerts | MVP | 💤 | One alert per transition; the first scan of a fresh install announces nothing |
| B6 | Scheduled-scan failure alerts | MVP | 💤 | |
| B7 | Alert acknowledgement | P2 | 💤 | API and notification suppression only; see §B7 |
| B8 | Maintenance / silence windows | P2 | 💤 | A `maintenance_windows` setting, one-off or recurring, optionally scoped to listed drives; evaluated at dispatch, so no idle cost |
| B9 | Self-test result notifications | P2 | 💤 | One `self_test_result` event, high priority on failure, deduped on the drive's own result identity |
| B10 | ntfy action buttons, priority, tags | P2 | 💤 | The button needs `public_url`, because the app cannot know its external address and a wrong link is worse than none |
| B11 | Gotify / Telegram / Discord / SMTP adapters | P2 | 💤 | Each a small `Sender`; the editor is data-driven, so another adapter is a data change |
| B12 | Per-channel event subscription | P2 | 💤 | Checkboxes served from the server's event list, plus a group scope naming which drives a channel is about; an empty list is every group, fleet-level events are never filtered, and `Test` ignores the scope |
| B13 | Weekly digest | P3 | 💤 | A schedule with `action: digest`; one message naming the fleet, what changed and what to act on |

### B7: acknowledgement

Stored per drive in `device_notes` as `ack_health`, `ack_note` and `ack_at`, scoped to the verdict
at the time and carrying a note saying why. The ack is spent when the verdict moves, so it can never
hide a drive getting worse. Acknowledged drives are excluded at dispatch. The API is
`PUT /api/devices/{serial}/ack` and `DELETE`; the payload carries `ack` wherever the verdict is
shown, but the rebuilt page has no acknowledge control.

## C. Operations and deployment

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| C1 | Docker image and compose reference | MVP | 💤 | |
| C2 | `/healthz` for Docker `HEALTHCHECK` | MVP | 💤 | Exempt from the auth gate, because a probe cannot carry a secret |
| C3 | SQLite single-file storage and retention pruning | MVP | 💤 | Snapshots and the notification log are pruned; `retention_days` also sets the analysis window |
| C4 | Reverse-proxy auth documented (nginx / Caddy) | MVP | 💤 | Still the answer for more than one user (`D23`) |
| C5 | Optional single-user sign-in | P3 | 💤 | Off unless configured; see §C5 |
| C6 | Prometheus `/metrics` exporter | P3 | ⚠ | `GET /metrics` in the exposition format, off by default (`prometheus_enabled`), hand-written to avoid a client-library dependency tree; see `API.md` |
| C7 | Import/export config, notes and labels (JSON) | P2 | 💤 | One bundle keyed by serial; see §C7 |
| C8 | Multi-arch image (amd64 / arm64) | MVP | 💤 | Home NAS and Raspberry Pi |
| C9 | Configurable base path | MVP | 💤 | `SMARTSERVE_BASE_PATH` (`ADR-015`); the auth gate sits inside it |

### C5: optional single-user sign-in (`ADR-033`)

Off unless `SMARTSERVE_AUTH_USER` and a password (`SMARTSERVE_AUTH_PASSWORD` or
`SMARTSERVE_AUTH_PASSWORD_FILE`) are both set; one half alone is a startup error. A browser gets a
sign-in page, an HttpOnly session cookie, a **Sign out** control and an idle timeout
(`auth_idle_minutes`, default 30), with a 24-hour absolute cap on any session. curl, Prometheus and
scripts send HTTP Basic and never see a session; a failed API request gets Basic, a failed browser
request is redirected to the sign-in page with `?next=`. `/healthz`, the sign-in page and static
assets are exempt. Credentials are compared in constant time, and a failure says only that the pair
did not match. There are no accounts, roles or user table.

### C7: config import and export

One JSON bundle of labels, settings and schedules, keyed by serial because that is the only
identifier that survives a host move. **Import merges**, with the file winning for what it contains,
so a restore that deleted anything it did not recognise cannot be run twice safely. Schedules are
the exception: they are replaced, and the UI says so. **Channels are excluded**, because their
config holds tokens and passwords and an export file is the thing that gets emailed around; the
bundle's own `note` says so. Knowledge overrides travel (`A21`); the fetched cache does not.

## D. Interface and use

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| D1 | Fleet table and device detail page | MVP | 💤 | One table, one detail page; the card grid and separate dashboard are retired |
| D2 | Theme: dark / light / auto / custom accent | MVP | 💤 | Plus Catppuccin (four flavours), Dracula, Nord, Gruvbox (light/dark), Synthwave (light/dark), Hacker, Solarized (light/dark), Tokyo Night and One Dark; custom base, accent and surface tint |
| D3 | Responsive mobile layout | MVP | 💤 | Sidebar becomes a drawer; the topbar exists only for the toggle |
| D4 | Hide a drive, or forget one entirely | MVP | 💤 | Hidden drives are left out of the fleet; forgetting removes the drive, its history and its notes, and is not undoable. Both are API only today |
| D5 | Sort, filter and search the fleet | P2 | 💤 | Every column sorts: click the header, click it again to reverse, `aria-sort` carries the state. The default is the reading order (failing, caution, no reading, healthy, then lower score, then name). A drive with no value for the sorted column goes last in both directions. The glance counts filter by state, and search covers name, bay, serial, model, tags, filesystem and mountpoint |
| D6 | Side-by-side comparison | P3 | 💤 | `GET /api/compare?serials=` aligns two to six drives by attribute id server-side, marking rows that differ and cells a drive flagged (`ADR-027`). The fleet-table picker that opened the dialog is not in the rebuilt fleet view |
| D7 | Localization | P3 | 💤 | **Not built, and deferred**: the cost is extracting every string and keeping every locale in step, which is only worth it for a translator who maintains the result (`ADR-023`) |
| D8 | Dense fleet table | P2 | 💤 | The fleet page itself: one row per drive with health word and score in one cell, usage, temperature, powered-on hours, and the row's own actions in a trailing column |
| D9 | Tags and group scope | P2 | 💤 | Tags are stored per drive and used for notification group scope and correlated heat (`A25`). The fleet's "group by tag" toggle did not survive the rebuild |
| D10 | Bay / slot field | P2 | 💤 | Free-text `bay` with the drive's notes, shown in the device facts, searchable in the fleet and first in every export. Not validated against a list of bays: the enclosure decides what its slots are called |
| D11 | Bench drives | P3 | 💤 | The `bench` tag is the session; see §D11 |
| D12 | Hide and forget drives | P2 | 💤 | Hide takes a drive out of the fleet and puts it behind the "N hidden" toggle, where the same table shows it dimmed with **Unhide**. Forget removes a drive and its history, and is offered only for a drive that is not connected, because the next scan would undo it for a connected one (`DELETE /api/devices/{serial}`) |
| D23 | Multi-user accounts and roles | P3 | 💤 | **Non-goal**: one operator, and a reverse proxy for anything more. The single-user gate (`C5`) adds no account system |

### D11: the bench

The tag **is** the session: no mode, no column, no control. A drive tagged `bench` is left out of the
fleet's health counts and out of the replace-first ranking, and stays listed, searchable and in every
export; being out of the summary is not being hidden. The exclusion is stated, not silent, because a
reader who adds the counts up deserves the reason. The server sends `on_bench` per drive and
`bench_tag` once, so the UI never splits a tag string. The batch verbs and the "being tested"
checkbox are not in the rebuilt interface: today the tag is set through
`PUT /api/devices/{serial}/notes`, and a batch file comes from `?tag=bench` on the fleet export.

## E. Idle-cost guardrails (rules for accepting a feature)

These rules exist so feature growth cannot erode the core promise:

1. **No periodic device reads** outside an explicit or scheduled scan.
2. **Time-based features sleep to the next event**, never tick on an interval: schedules and
   retention are a single `time.Sleep`, and something needed only when another event happens
   (maintenance windows, squelching) is evaluated at that moment.
3. **Any scraper-style endpoint** ships disabled by default (Prometheus).
4. **UI freshness is tied to the last scan**, never to live polling, and the last-scan time is
   visible on the fleet and the device page. There are three narrow exceptions:
   - the self-test card polls the drive on `selftest_poll_seconds` while a test it started is
     running and the card is on screen, stopping when the test ends or the card leaves the page;
   - a server-side watcher re-reads drives already mid-test on the same interval and stops when the
     last one ends;
   - opening a drive re-reads it when its data is older than `device_refresh_minutes` (one drive,
     once per window, `0` never).
5. **Anything that writes to a drive is opt-in and never automatic**: the user presses the button or
   writes the schedule (`A19`).

## F. Explicitly rejected (`SCOPE.md` non-goals)

- Write operations to drives (settings changes, secure erase, formatting).
- RAID-controller S.M.A.R.T. passthrough (`-d megaraid`, ...).
- udev / hot-plug watcher.
- Host CPU, RAM or network metrics: scope creep away from S.M.A.R.T.
- Per-filesystem usage graphs. One storage utilisation figure per drive is charted (`A28`); a graph
  per filesystem is not S.M.A.R.T. and is not built.

## G. Drive knowledge base strategy

The knowledge base ships as a bundled table maintained in-repo, optionally refreshed from a URL and
extended by the user, with no runtime polling and no idle cost. The layers, the matching rules and
the data model are in §A21. Graceful degradation is the floor: a drive absent from the table still
gets the full S.M.A.R.T. report, and no drive is hidden or downgraded for lacking knowledge data.

## H. Tabled (recorded, not planned)

| # | Feature | Why it is tabled |
|---|---|---|
| H1 | Batch self-test ("test all N drives overnight") | **Built** as the scheduled self-test pass (`A19`, `ADR-024`): one fire tests every eligible drive, one at a time in serial order. The pass is a schedule rather than a button, deliberately, because only a schedule the user wrote should write to every drive unattended |

## I. Sequencing: documentation, implementation, interface polish

Feature work lands in three moves, and the third is not optional:

1. **Documentation.** An idea is written into this registry (and `SCOPE.md`, plus an ADR when it
   carries a real trade-off) before it is built, so it is recorded rather than rediscovered.
2. **Implementation**, with the test discipline the rest of the project carries.
3. **Interface spot-cleaning.** Every batch adds controls, and an interface that grows
   feature-by-feature stops being sleek. After each batch the interface is reviewed as a whole
   (grouping, hierarchy, what to demote into a secondary surface) and the result recorded in
   `UI.md`.

This is a working agreement rather than a phase: it repeats for every batch (`ADR-019`).
