# Smart-Serve — Feature Catalog & Suggestions

A comprehensive inventory of features for Smart-Serve, including proposals beyond the
original brief. Tiers follow `SCOPE.md`. Every feature must preserve the
**zero-idle-cost invariant** (`DESIGN.md` §2.1): nothing here may poll devices or run
timers while the app is idle.

**Legend** — tier: `MVP` / `P2` / `P3`; idle cost:
💤 adds no idle cost · 🔌 only active during a scan · ⚠ needs an opt-in guard to
avoid idle cost.

**Status** lives in the Notes column. Every feature in tiers A–D is shipped except `D7`, which says
so. A blank Notes cell means the entry predates the column, not that the feature is missing.

**What is left in the whole app**, gathered here because it was previously spread across five files:

| What | Where it is recorded | Why it is open |
|---|---|---|
| `D7` localization | `SCOPE.md`, `ADR-023` | Deferred, not dropped: extracting every string and keeping every locale in step is only worth doing for a translator who will maintain the result |
| Error-log **entry lists** (`A20`) | §A20 above | The counts and the self-test entries are shown; smartctl emits no list at all for an empty log, and neither drive available here has ever logged an error, so the populated shape has never been captured |
| arm64 on real hardware | [TESTING.md Pass 8](TESTING.md#pass-8--arm64-raspberry-pi), `SCOPE.md`, `RELEASE.md` | **Partial verification on Pi 5 / DietPi (2026-11-08):** native arm64 execution, startup health, a full container SMART read through USB SAT, and identity/attribute checks confirmed for the tested disk. That session surfaced two defects — the self-test progress bar sticking at a percentage the drive's own log had already moved past, and an SD-card page that re-read itself in a loop — **both fixed and retested on 2026-11-11, on the Pi and on amd64**. The self-test symptom was never ARM-specific. Idle-budget acceptance, overnight stability and broader parity remain open. |
| Multi-user accounts / roles | `SCOPE.md` Phase 3 | **Conflicts with `D23`** — authentication is delegated to a reverse proxy, so this needs that decision revisited rather than built |

Everything else recorded in these documents is either shipped or an **explicit non-goal** — writing
to drives, RAID-controller passthrough, udev hot-plug detection, host CPU/RAM metrics, and built-in
authentication (`SCOPE.md` §Explicit non-goals). Those are decisions, not backlog.

## A. Core reporting (the S.M.A.R.T. mission)

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| A1 | Device list with `lsblk` parity (internal + USB) | MVP | 🔌 | lsblk is source of truth |
| A2 | On-demand **Scan** button with live SSE progress | MVP | 🔌 | |
| A3 | Device detail: full attribute table (value/worst/threshold/raw) | MVP | 💤 | CrystalDiskInfo layout. **Thresholds can also be set per drive model** — the global defaults are a compromise across everything in one box, and an entry says "this model, except…". It moves the verdict only: the report's evidence stays the drive's own manufacturer threshold (`A27`) |
| A4 | Overall verdict Good/Caution/Bad with plain-language rationale | MVP | 🔌 | |
| A5 | Temperature, power-on hours, power cycles, capacity | MVP | 🔌 | |
| A6 | Firmware version, serial, WWN, interface speed display | MVP | 💤 | from smartctl |
| A7 | Human-readable drive age ("in service 4.2 years") | MVP | 💤 | derived from power-on hours |
| A8 | Copy / export S.M.A.R.T. report | MVP | 💤 | **done** — plain text, self-contained web page, `field,value` spreadsheet, or JSON, chosen from an Export dialog. Every format **opens on the drive** and closes with what the file is for |
| A9 | Health summary bar (n good / n caution / n bad) | MVP | 💤 | **done** — folded into the Devices header as a compact clickable list beside the title and search box |
| A10 | Raw `smartctl` output viewer (diagnostics toggle) | MVP | 💤 | vital for USB bridge quirks |
| A11 | Per-device transport override (`-d sat`, …) | MVP | 💤 | **done** — offered on the device page for any drive whose S.M.A.R.T. cannot be read, and shown as currently set |
| A12 | Attribute help tooltips ("what is Reallocated Sectors?") | MVP | 💤 | **done** — a curated `?` per attribute and per NVMe counter, a marker for attributes the *drive itself* reports at threshold, and the score breakdown it turned out the scan had been computing and discarding all along; see below |
| A13 | Scan history log (durations, per-device errors) | MVP | 💤 | |
| A14 | Degradation trend detection ("worsening" early-warning badge) | P2 | 💤 | **done** — see below |
| A15 | History charts (temp, sectors, wear) + sparklines | P2 | 💤 | **done** — per-metric sparkline card on the device page, with a 7/30/90-day/all range selector. The x-axis is **time**, not reading index, with one shared date axis under the card (`ADR-023`) |
| A16 | Remaining-longevity estimate (NVMe / SSD / HDD) | P2 | 💤 | **done** — see below, with confidence labels |
| A17 | Health score 0–100 + letter grade | MVP/P2 | 💤 | **done, trend-aware** — the score is lowered by up to 10 points for a drive whose bad sectors are climbing, with the reason in the rationale. See below |
| A18 | Snapshot diff ("what changed since last scan") | P2 | 💤 | **done** — a "Since the last scan" card on the device page. The server decides which attribute movements are findings, so the diff cannot disagree with the verdict. See below |
| A19 | SMART self-test (manual short/long + scheduled) + self-test log | P2 | 💤 | **done** — short/long, live progress, abort, and a `selftest` schedule action that runs one drive at a time; opt-in only, never automatic. See below |
| A20 | SMART error log display | P2 | 💤 | **done** — the error and self-test log *counts* and *entries*, the drive's own self-test timings, and smartctl's words when a log cannot be read. See below |
| A21 | Drive knowledge base (rated TBW, known firmware advisories) | P3 | 💤 | **done** — a bundled table, an opt-in refresh from a URL, and your own entries. Rated endurance on the device page, advisories only when the firmware matches, and every figure carries its source. Never fetched on a schedule. See below |
| A22 | Usable support for drives with no S.M.A.R.T. (USB flash drives, card readers) | P2 | 💤 | **done** — see below |
| A23 | Fleet-wide export (every drive in one CSV or page) | P2 | 💤 | **done** — one row per drive, as a CSV or a printable page. A missing reading is an empty cell, never a zero. See below |
| A24 | "Replace first" ranking (verdict + direction + remaining life, ordered, with reasons) | P2 | 💤 | **done** — a "Replace first" card, using bands rather than a weighted score so the reasons can be shown. See below |
| A25 | Correlated-change detection (N drives in a group moving together → one alert about the enclosure) | P3 | 💤 | **done** — temperature only, because that is what an enclosure shares. Two settings, an opt-in notification, and wording that claims a correlation rather than a cause. See below |
| A26 | Projection window follows retention (or becomes a setting) | P2 | 💤 | **done** — the projection, the worsening flag and the trend adjustment all use the `retention_days` setting. See below |
| A27 | Warranty / RMA evidence pack (the exported report says what it is for) | P2 | 💤 | **done** — every format says what it is for, and names the evidence: the attributes failing the drive's *own* threshold, and the counters the app reads. The saying is at the **foot** of the file, so the drive's own data leads |
| A28 | Partition schema and storage usage per drive | P2 | 💤 | **done** — the `lsblk` tree is kept instead of flattened away. See below |

### A28 — Partition schema, storage usage, and the rest of what we already fetch

The app asks `lsblk` for the whole device tree and then flattens it: `collectTree` walks every
partition to gather mountpoints and filesystems into two strings, and the tree itself — sizes, types,
labels, offsets, sector sizes — is dropped on the floor. This is that tree kept, and the audit of
everything else in the same position.

**The rule this is built to.** If a fact is already on the wire, the app either shows it or says why
it does not. Fetching something and discarding it is the one outcome that is never right: the cost is
paid on every scan and nothing gets the benefit.

**Shown on the device page, per drive.** One partition per row: the node name, its size, its
filesystem and version, its label, where it is mounted, how full it is, and — where the partition
table says so — what the partition is *for*.

| Field | Source | Why |
|---|---|---|
| node, size, type | `lsblk` children | The schema itself. A 2 TB drive with a 200 GB partition and 1.8 TB unallocated is a different drive from a full one, and nothing said so before |
| filesystem, fs version | `lsblk` `FSTYPE`, `FSVER` | Already gathered, but aggregated to a comma-joined string with no idea which partition it came from |
| label, partition label | `lsblk` `LABEL`, `PARTLABEL` | What the user called it. `tank`, `backup`, `EFI` — this is the field that makes a partition row mean something to a person |
| mountpoint | `lsblk` `MOUNTPOINTS` | Which partition is `/`, which is the pool |
| used, free, size, use % | `lsblk` `FSUSED`, `FSAVAIL`, `FSSIZE`, `FSUSE%` | Storage usage. `null` when the mountpoint is not visible to the container, rendered as *"not visible to the container"* rather than as a blank or a zero |
| partition type | `lsblk` `PARTTYPENAME` | "EFI System", "Linux filesystem", "Linux LVM", "Linux RAID member". The partition table describing itself — not the app monitoring RAID health, which stays out of scope |
| read-only | `lsblk` `RO` | A card with its write-protect switch on, or a device that has failed into read-only |
| state | `lsblk` `STATE` | `running` vs a device that is present but not live |

**A "copy UUID" button, not a UUID column.** `UUID` and `PARTUUID` are captured and are the one thing
in this list that is deliberately *not* shown: as columns they are two long opaque strings per
partition with nothing to scan. But they are what `/etc/fstab` speaks in, so the row carries a button
that copies one — the filesystem UUID first, the partition UUID as the fallback for a partition that
has no filesystem, which is exactly the volume-group member somebody is most likely to be copying one
for. A button is the convenience; a column would have been the verbosity.

**Storage usage needs one line in the compose file.** `lsblk` gets `FSUSE%` from `statvfs()` on the
*mountpoint*, so it is populated only where the container can see it. `/dev:/dev:ro` does not do it.
Add the mount roots read-only — `- /mnt:/mnt:ro`, or `- /:/host:ro` — and usage appears. Without it
the row says so, which is the honest answer and the one the app already uses for absent values.

**A "Details" disclosure for the power-user facts.** Three facts are worth having and not worth
putting in front of everybody: the logical and physical sector size (512e vs 4Kn, which decides
whether a drive can join a pool that demands one), and whether the device reports TRIM support
(`DISC-GRAN`, `DISC-MAX`, `DISC-ZERO` — an SSD behind a bridge that does not pass discard through will
slow down over time, and that is a real USB-enclosure finding). Collapsed by default, in the same
place, so the main surface stays a verdict and a table.

**`in_smartctl_database` is now a fact, not an inference.** The attribute table already labelled
unrecognised rows "Vendor specific" and said smartctl did not know the model — but it *inferred* that
from seeing `Unknown_Attribute` names, so it said the same thing in three different situations. The
field separates them: **false** is a model smartctl has never heard of and has no names for at all;
**true** is a model it knows whose vendor keeps *these particular* counters private; absent is a
smartctl that did not say, where the note states only what is observable. It rides in the snapshot's
attributes blob rather than on the device, because it is a fact about that *read* and the attribute
names are what it accounts for.

**Settings.** Two toggles, both on by default: `show_partitions` and `show_usage`. They exist because
the information is not doubtful and the verbosity is — a reader who only cares about the drive as a
whole can turn the card off, and the usage column is *dropped* rather than emptied, because an empty
column is a worse way to say "you asked not to see this" than no column.

**A partition that is not mounted is not "not visible".** Three genuinely different answers live in
that one cell: nothing mounted means nothing to measure (a dash); mounted but outside the container
means there is no figure to read ("not visible", with the compose fix in Details); mounted and visible
means the number. Conflating the first two would send somebody to edit their compose file for a
partition that has no filesystem at all.

**Storage over time.** `snapshots.used_pct` (migration `0016`) records the fullest filesystem's
percentage at every scan, so the trend card gains a **Storage** row. Denormalized out of the device's
partition schema for exactly the reason `sectors` was denormalized out of `attributes`: the schema
lives on the *device*, so the chart would otherwise have nothing to read at chart time, and a trend
chart must never load a blob to draw a line. It is the one storage figure worth watching — a drive at
40% today and 90% next month is the thing that runs out of room. The delta is in **points**, not per
cent, because the value is already a percentage.

**The drive's own self-test status outvotes the self-test log.** A running test is reported in two
places: the log's newest row carries a `remaining_percent`, and `ata_smart_data.self_test.status` is
the field the ATA spec defines for exactly this. The log row must not win — a drive holding a leftover
percentage from an interrupted test would report itself busy **forever**, at a figure that never moves.
The status field wins **in both directions**: when it says a test is executing, that is the running test
and the log's leftover percentages are cleared; when it says nothing is executing, no log row is a
running test either. The rows are kept rather than dropped, because their result text ("Aborted by
host") is the explanation. Absent status is *silence* rather than a statement of idle, and the log is
all there is — reading an absent field as "not running" would break every drive that reports progress
only in the log.

**A drive whose self-test log was read is not told it could not be.** The unread-log notice was raised
by any unrecognised key containing `self_test`, and that set includes
`ata_smart_selective_self_test_log` — the *ranges* a selective test covers, which this app never starts
and which most modern ATA drives report. Nearly every ATA drive therefore carried a sentence saying its
self-test log could not be read while its results were parsed correctly beside it. The notice is raised
only when the drive offered **no** self-test log this app reads.

That flag also **suppressed the card's live read**, which is what made it visible: the read was skipped
on the reasoning that it "could only fail". It could not — `smartctl -x` reads every log, and only the
*parser* is limited — and the live read is the only thing that corrects a progress bar drawn from a
stale scan. The card reads the drive on mount whatever the logs look like, and a log that genuinely
cannot be read is still said out loud.

**A running test is re-read, so the badge tracks it.** The fleet badge and a drive page's progress both
come from the *stored* row, which only a scan writes — so starting a long test and looking at another
drive left the badge frozen at whatever the last scan caught, which on a quiet homelab could be
tomorrow. A server-side watcher now re-reads **only drives already mid-test**, on the self-test refresh
interval, and publishes the change over SSE. Three properties make it safe rather than a second scan:
it cannot wake a sleeping drive, because it only ever looks at drives already working; it reads one
document and writes three columns rather than a whole row, so it does not fight a scan; and it stops by
itself when the last test ends, because the query it runs then returns nothing.

**The disk map answers a mouse immediately, and is linked to the table both ways.** The browser's own
tooltip takes about a second to appear, which is wrong for something you sweep across, so the label is
the app's own. Pointing at a segment marks the table row it stands for, and pointing at a row marks the
segment it came from — the same link in both directions, so the bar and the list read as one thing
whichever end you start from. Matched by the drive's own name for the partition rather than by position,
because a table carrying logical volumes has more rows than the map has segments, deliberately so — and
a row with no segment (a volume group, which the map does not draw) simply marks nothing. The heading
carries only a count.

**A drive's data is re-read when it has gone stale.** Opening a drive page whose last scan is older
than `device_refresh_minutes` (default 15) reads *that drive* and redraws — one smartctl call, not a
fleet scan. It is the only thing in this app that touches a drive nobody asked it to touch, which is
why it is a setting, why 0 means never, and why the page says when it is doing it. On a machine that
parks its disks this is the number to turn down: a scan cannot wear a drive, but it can wake one. A
drive with no data at all is read regardless — the window exists to stop *re-reads*, and applying it to
a first read would leave the page empty for no reason.

**Once per window, and that is a fix rather than a detail.** The re-read redraws the page, the redrawn
page re-reads the stale drive, and the only thing that ends that is data arriving — which an unreadable
drive never produces. `last_scan` is advanced by a read that *succeeded* and nothing else, so a drive
that cannot be read is stale permanently and the age can never be the guard. There is at most one attempt
per refresh window per drive, so a drive that comes back is picked up on a later visit rather than being
written off for the life of the tab.

The scan's age now sits beside the drive's name rather than in a row of twelve facts, because "4 hours
ago" among twelve is not where a reader notices it.

**A map of the disk, above the table.** One segment per top-level partition, sized by how much of the
disk it takes, with the partition's own fullness filled in from the left — so one glance answers both
"what is on this disk?" and "where is the room?". The table underneath is the detail for the segment
somebody just pointed at. Three things it is careful about, each of which would otherwise make it
lie:

- **Top-level partitions only.** The flat list includes logical volumes and dm-crypt mappings, which
  live *inside* a partition. Drawing one beside its container would claim the disk holds more than it
  does. This is where the earlier "no depth field is needed" had to be revisited: right about the
  table, wrong about a map. `top_level` is one boolean, and it is the minimum that restores what the
  map needs.
- **Unknown fullness is not zero.** A segment whose filesystem the container cannot see is hatched,
  because a bar at 0% would say "this partition is empty" about one nobody could look at.
- **Free space is approximate, and says so** — and is only drawn when it is a *share* of the disk.
  Below a percent it is an alignment remainder (the 1.4 MB left after a partition table), which is
  real and is not free space in any sense a reader would use; drawing it gave a visible sliver to a
  number nobody acts on. The map's background shows through there instead.

**⚠ The array-member recognition is unverified against a real array member.** The mapping from a
whole-disk signature to a plain-language name (`linux_raid_member` → "a Linux md array", and the ten
other array and pool signatures) is written from blkid's own token names, and it has **never been seen
working on a disk that actually carries one**. The drives available for testing here either have a
partition table or are genuinely unpartitioned, so the "member of …" path has only ever been exercised
against fixtures. The mechanism is sound — the token is `lsblk`'s own `FSTYPE` and the names are
blkid's — but *sound is not verified*, and this should not be called a working feature until somebody
opens a drive page for a disk pulled from an md array, an Intel RST array, or a ZFS pool and sees the
right sentence. The same caution applies to the pool signatures (`zfs_member`, `LVM2_member`,
`crypto_LUKS`, `bcache`).

What *is* verified: that a partitioned disk reports no whole-disk signature (so the page cannot claim a
drive holds a "gpt filesystem"), that an unknown signature falls back to the old message rather than
guessing, and that a drive with a filesystem written straight to it is described as such.

**A drive with no partition table says what it holds.** "Unpartitioned, or its table could not be
read" is true and useless, and it is what a disk pulled from a RAID array produced — for a drive
doing exactly what it was built to do. An array member has no partition table *because* it is an
array member, and `lsblk` already reported the fact: the disk node's own `FSTYPE` is
`linux_raid_member`. That signature is kept (`whole_disk_fstype`) with a plain-language name beside it
(`whole_disk_name`), and the card says "a member of a Linux md array" rather than implying the drive
is unset-up. The same path covers a ZFS pool member, an LVM physical volume, a LUKS container, and a
filesystem written straight to the device — all of which have no table and all of which are things a
person can recognise once named. It is kept **only when there is no partition table**: on a
partitioned disk the node's own signature *is* the table, and reporting it would say the drive holds
a "gpt filesystem".

**The card's summary line was wrong.** A heading reading `GPT partition table · 1.4 MB unallocated ·
512e` puts facts about the *drive* above a table of *partitions*: they answer no question anybody has
while reading one, and the unallocated figure is an alignment remainder that invites a question with no
useful answer. A summary position has no room to explain an approximation, so an approximation does not
belong in one. The heading carries a count; the table type moved to Details, where it explains the
`PARTTYPENAME` values above it; the unallocated figure moved there too, where there is space to say it is
approximate. **A summary line is a claim that a fact is important enough to lead with — and "this is on
the drive" is not the same as "you need this".**

**The explanation moved into the disclosure.** The "N of these filesystems are mounted on the host but
not inside this container…" paragraph sat under the table and was the most visually heavy thing in the
card. It is now a row in **Details**, and the cell itself carries the short version as a tooltip. The
tradeoff is real — somebody who sees "not visible" in the table has to open Details to find out why —
and it is accepted because the *cell* still names the condition rather than showing a dash, and the
explanation is one click away rather than absent.

**The bar turns amber at a level the user sets, and that level is a stored setting.** The first
version was deliberately one colour, on the argument that a bar turning amber at 85% is the browser
re-deriving "is this bad?" — which the attribute table avoids by having the verdict computed
server-side. That argument was wrong for *this* number. Fullness is not health: a drive can be 95%
full and perfectly healthy, and the two answer different questions, so a coloured bar cannot
contradict the health badge — they are not saying the same kind of thing. What was right is where the
number lives: `storage_warn_pct` is a setting, so the browser is drawing the line the user drew
rather than inventing one. Default 90, adjustable 50–100, on the **Thresholds** page because that is
where somebody looks for a number like it. It does **not** affect the verdict, and it is deliberately
not in the per-model seed — that list is the interpreter's own numbers, and offering every model an
override for something the scan never reads would be a control that does nothing.

**Deliberately not captured, and why.** These are decisions rather than omissions:

- **`UUID` / `PARTUUID` / `PTUUID`.** Machine identifiers for `/etc/fstab` and `blkid`, which answers
  the question in one second. In the UI they are two long opaque strings per partition with nothing
  to scan, and no part of this app consumes them.
- **`ata_smart_data.offline_data_collection`.** The drive's own background surface scan, which this
  app neither starts nor manages. Its usual value on a healthy drive is *"was never started"*, which
  reads as a warning to anyone who is not already an expert. A field whose normal state looks like a
  fault is a field that costs more than it pays.
- **`device.protocol`.** Redundant: `model.Bus` already answers ATA / NVMe / SCSI, derived from the
  JSON's own fields. It was decoded into two structs that nothing read, and both have been deleted —
  dead code is the same failure as a discarded fact, one layer further in.

### A14/A16 — Longevity and the worsening badge

Both come from the same history, and both are deliberately modest, because a S.M.A.R.T.
reading is a snapshot of a device that does not know its own future.

**Longevity** is extrapolated from the drive's own wear counter (NVMe `percentage_used`),
reported as a horizon — *"80% of rated life left, about 320 days at the current rate"* —
with a confidence that reflects how long the drive was actually watched (`low` under a
month, `medium` beyond) and its caveat shown **inline**, not behind a tooltip: a caveat
nobody reads is not a caveat. A window shorter than a day produces no rate at all, because
two scans an hour apart say nothing about remaining life.

**Sector growth is a rate, never a date.** Counts arrive in bursts, and a date
extrapolated through two of them looks precise and means nothing — so the app reports
*"7 new sector problems in 60 days"* and says why there is no forecast. For the same
reason, ATA wear counters are left alone: they are vendor-specific enough that guessing at
one is worse than reporting none, so an ATA drive falls back to the sector basis.

**The worsening badge** (`A14`) fires when bad sectors are accumulating, on the dashboard
card and the device page. It is separate from the verdict badge because the two answer
different questions: the verdict says how the drive looks *now*, the badge says which way
it is going — and a drive can be green today and still be the one worth replacing first.

**The score is trend-aware** (`A17`). A verdict is a statement about now; a score is a
summary, and a drive whose bad sectors are climbing is worse than one sitting at the same
count and going nowhere. So the score takes a penalty — 3 points for the first new sector,
then one more per doubling, capped at 10 — and the rationale says why, as a rate and a
window rather than a date.

Three things about that are deliberate. The penalty is **bounded**, because a trend is an
extrapolation from a handful of readings and a score is the number people quote and
compare; the cap is also small enough that the trend alone can never carry a drive out of
its verdict, so it adjusts the summary rather than overruling the drive's own data. It is
**applied outside `interp.Evaluate`**, in the scan, because the trend is a question about
*history* and `Evaluate` is deliberately a pure function of the drive's own data. And it is
**skipped for a drive that could not be read**, since a trend inherited from an earlier scan
is not evidence about a drive nobody just looked at.

A drive with too little history says so rather than guessing, and a drive that reports no
sector counts is *absent* from the trend rather than reported as flat: no history is not
the same finding as a clean bill of health.

### A18 — What changed since the last scan

One card on the device page, answering one question. Three things about it were decisions rather
than implementation.

**"Nothing changed" is a real answer.** A drive scanned twice and found identical is exactly the
reassurance someone opened the page for, so the card says so in words. An empty panel would read as
"we did not look", which is the same distinction the drive-logs panel and the trend card already
make.

**Absent is not zero.** A drive that reported no temperature last scan has no *from*, and the card
renders an em dash. Showing 0 °C would invent a reading the drive never gave, and this app exists
not to do that.

**The server decides what counts as a finding**, and it invents nothing: an attribute movement is
trouble if the drive's *own* threshold is met, or if it is one of the lost-storage counters the
interpretation already sums (5, 197, 198). A second opinion in the UI would let this card and the
verdict disagree about the same drive, which is the one thing an attribute-level judgement must not
do. Everything else that moved is **counted and named as a count** — "4 other counters moved, which
is ordinary wear" — rather than dropped, because a silent filter is a lie by omission.

A counter the drive has just started reporting is marked *new*, which is the difference between
"nothing was wrong before" and "we were not being told".



Most USB flash drives and card readers do not implement S.M.A.R.T. at all, and some USB
bridges do not pass it through. `smartctl` fails on them, and the app cannot invent health
data, so it does the two things that are actually available:

- **Say so plainly.** The device page shows a neutral **notice**, not an error box: the
  drive does not report S.M.A.R.T., here is why, and the identity, capacity, filesystem
  and mountpoints shown come from the operating system rather than the drive. The
  dashboard card carries a *No S.M.A.R.T.* badge instead of *Unknown*, which would read
  as "not worked out yet", and no health ring is drawn because there is no score to draw.
- **Report the facts lsblk has.** `removable`, `fs_version` and lsblk's `REV` (used as
  the firmware revision when smartctl cannot supply one) are stored alongside the rest.
  For a flash drive these are most of what can be said about it, and they cost nothing —
  `lsblk` already ran.

The notice also carries the per-device `-d` **transport override** (`A11`), because a
real SSD or HDD inside a USB enclosure usually *does* have S.M.A.R.T. and just needs the
right bridge protocol.

Deliberately **not** done: filesystem-usage graphs, write/read benchmarks, or a synthetic
health score for a drive that reports nothing. Those would be inventing data the drive
never provided, which is exactly the failure mode this app exists to avoid.

### A12 — Attribute help

An attribute table is the densest thing this app shows, and it is unreadable to anyone who
has not already spent time with CrystalDiskInfo: *"Reallocated Sector Count 100 100 10 0"*
is four numbers and no meaning.

So each row carries a **`?` that opens a short explanation** — what the field is, and how to
read it. The "how to read it" half is the part that matters, because it is what stops a
benign vendor counter from looking like a failure: attribute 199 is nearly always a SATA
cable rather than a dying drive, and the raw figure for attribute 1 is not comparable
between manufacturers at all. The affordance is a **button, not a `title` attribute**,
because hover-only help is unusable on the mobile layout.

The table is **curated, not exhaustive**, and it mirrors the fields the interpreter actually
acts on (`SMART_INTERPRETATION.md` §1.1 and §2). Explaining a row the verdict ignores is
noise, and a description that disagreed with the verdict would be worse than none — so a
test asserts that every interpreted attribute has help text, and the two lists cannot drift
apart silently. An attribute with no entry still renders exactly as before; it simply has no
`?`.

One attribute-level judgement *is* made in the UI, and deliberately only one: a row is
marked when the **drive itself** reports the attribute at or below the threshold the drive
publishes for itself. That is the drive's own alarm condition rather than a threshold this
app invented, and it is the only such judgement because the verdict and the score are
computed server-side — re-deriving "is this bad?" in the table would let the two disagree
about the same drive. A threshold of 0 means *no threshold*, not *fails at zero*, so
informational attributes are never flagged.

**Done, and it was worse than "worth doing".** The score breakdown (`interp.Penalty`) is a per-drive
list of reasons and the points each cost, and the scan has always produced it and then thrown it
away — computed on every read, never stored. So the one question a 0-100 score invites, *"why?"*, had
an answer that existed for a few milliseconds. It is now kept (`devices.penalties`, migration 0014),
sent on the device endpoint, and shown as a one-line disclosure under the verdict. See `ADR-029`,
which also records why the breakdown is *stored* rather than recomputed on read: a page that says
"72/100, because of these three things" must have the three things that made it 72, and a breakdown
recomputed at read time can disagree with the score beside it. The same pattern — a computed fact
being discarded — then turned out to have two more instances, which became `ADR-028`.

The `attribute_id` came with it: the attribute table marks the rows the verdict actually **read**, so
a row can now be either *"at threshold"* (the drive's own alarm), *"counted"* (part of this app's
score), both, or neither. Two different claims, worded differently, because conflating them was the
whole reason the marker could only ever be a guess.

**The trend adjustment is in the breakdown too.** `applyTrendPenalties` lowers a score after the
interpreter has finished, so a breakdown without it would sum to *less* than the score it claims to
explain — the one thing a breakdown must never do. It carries no attribute id: the trend is the total
of attributes 5, 197 and 198 moving, which is not a statement about any one row.

### A20 — The drive's own logs

The question this answers is *"has this drive ever complained?"*, and the honest answer turns
out to be a **count** rather than a list.

**What is shown.** How many entries the drive's error log and self-test log hold, the drive's
own estimate of how long its tests run (`short ≈ 2 min · extended ≈ 133 min` on an 850 EVO),
whether the drive supports self-tests at all, and smartctl's own words when a log could not be
read. A count of **zero is shown rather than hidden**: *"this drive has never complained"* is
the answer someone came looking for, and omitting it leaves them wondering.

**What is not shown, and why.** The entry lists. Neither drive available to this project has
ever logged an error, and smartctl emits no entry list at all when a log is empty — so the
populated shape has never been observed. Guessing it would produce a panel that renders
nothing precisely when a drive has something to say, which is the one moment it matters. The
raw document is stored verbatim for every drive (`A10`) and the panel says so, so the entries
stay reachable; entry rendering lands when a populated log is captured.

**What the real hardware taught us** — both fixtures are captures, not fabrications:

- `-x` already reads the ATA **extended** comprehensive error log. The summary log is often
  reported as supported while being permanently empty, so the extended one is preferred.
- NVMe has no equivalent. The SMART/Health log carries a count (`num_err_log_entries`) and the
  Error Information log emits only `{size, read, unread}` — a real SN730 reports **one** entry
  and prints none, with or without `-l error`. For NVMe the count is all there is.
- A drive with no self-test log says so **only** through smartctl's message
  (`Read Self-test Log failed: Invalid Field in Command`). Nothing else in the document
  reports it, which is why a message match carries that one fact.
- `self_test.polling_minutes` is the drive's own progress basis — exactly what `A19` needs in
  order to show a percentage instead of a spinner.

**Derived on read, not stored.** A log summary describes the drive's *current* state rather
than a point in a series, so it is parsed from the stored raw document when the device page
asks for it: no migration, no snapshot column. It is omitted from the device *list* response
so a dashboard refresh does not parse every drive's document for data the dashboard does not
show.

**Entry rendering, and why the two logs are shown differently.** A populated ATA error log was
finally captured from a second host (`ata-bto-1tb-logs.json`), and it decided how each log is
presented:

```json
"ata_smart_error_log": {
  "extended": { "revision": 1, "sectors": 1, "count": 1, "logged_count": 1,
                "table": [ { "error_number": 1, "log_index": 0 } ] }
}
```

On a drive smartctl does not recognise (`in_smartctl_database: false`), a logged error carries
**an error number and an index and nothing else** — no LBA, no timestamp, no status. So the error
log renders a **table only when its entries carry something beyond which entry they are**; when
they do not, the count stands alone with a sentence saying the drive reported no detail. A table
of dashes would look broken, and it would say less than the count already did.

The **self-test log is a different shape entirely**: `type`, `status` (including
`remaining_percent` while a test runs) and `lifetime_hours`. It is rendered in full, and it is the
record `A19` writes to.

**A test in progress is not a failure.** The drive reports no `passed` field while a test is
running, and treating an absent verdict as `false` would put a red cross beside a drive that is
merely busy. The three cases — passed, failed, in progress — are distinct in the model rather than
collapsed into a boolean.

### A23–A27 / C7 / D8–D11 — The NAS fleet

The MVP is sized for a workstation with a handful of drives. A NAS changes the *shape*
of the problem; each item below carries the reason it exists.

- **The dashboard has to scale.** A card grid is right for four drives and wrong for
  twenty-four; what a fleet owner wants is one list ordered by risk (`D8`), which the
  existing filter/sort work (`D5`) feeds.
- **Grouping is the organising idea** (`D9`). Tag drives by pool — `tank`, `backup`,
  `media` — and the fleet reads as three groups instead of twenty-four cards. Grouping
  is also the precondition for `A25`. **Built**, as a toggle offered only when something
  is actually tagged, and deliberately *not* a filter: a filter answers "show me only
  this", which the health chips and the transport select already do, while grouping
  answers "let me see the shape of the fleet". A drive with more than one tag appears
  under **each** of them, which means the group counts can add up to more than the fleet
  size — stated in the header rather than left to be discovered. The alternative,
  first-tag-wins, is tidier but silently drops a drive from a group it is tagged for, and
  the whole reason to group a fleet is not to miss one. Untagged drives come last, because
  they are the ones with something left to do to them.
- **A drive needs a physical address** (`D10`). A serial number identifies a drive; a
  bay number *finds* it. Cheapest NAS feature, largest payoff at the moment something
  actually fails. **Built** — free text ("bay 4", "front top", "USB port 2"), stored with
  the drive's notes so it survives an unplug, leading the card and present in every export.
  Deliberately not validated against a list of bays: the enclosure decides what its slots
  are called, and this app has never seen one. A drive with no bay gets no row at all,
  because a gap in the bookkeeping is not a finding about the drive.
- **Labels must outlive the container** (`C7`, promoted from P3). Names, notes and tags
  live in the SQLite volume, so they survive a restart but not a host migration. For a
  fleet, re-labelling sixteen drives is the difference between a five-minute move and an
  evening's work. **Built** as one JSON bundle of labels, settings and schedules, keyed by
  serial because that is the only identifier that survives a move. Two decisions:
  **import merges**, with the file winning for what it contains, because the usual reason to
  import is a fresh install that needs its labels back and a restore that deleted anything
  it did not recognise is one you could not safely run twice — except schedules, which are
  replaced and say so. And **channels are deliberately excluded**: their config holds tokens
  and passwords, an export file is exactly the thing that gets emailed around, and the app
  already refuses to serve secret values over the API. The bundle says so in its own `note`,
  so a file found in a drawer a year later explains itself.
- **Correlated change is the fleet-only insight** (`A25`). Eight drives in one enclosure
  share a temperature: a dying fan presents as *all eight* running hot, which per-drive
  monitoring reports as eight unrelated alerts — precisely the noise that teaches people
  to ignore alerts. One alert naming the group and the shared delta points at the
  enclosure instead of the drives. It is deliberately P3: it needs grouping and history
  first, and it needs a high bar, because a RAID rebuild legitimately heats everything.
  **Built**, and the whole design is one admission: **the app cannot tell a failing fan
  from a RAID rebuild, or from a warm afternoon.** Nothing observable from a temperature
  separates them. So it does not try. It reports a **correlation and never a cause** —
  "4 of the 4 drives tagged `tank` are at least 8 °C hotter than at the previous reading.
  They share an enclosure, so the cause is more likely to be there — a fan, a filter, or
  ambient — than in 4 drives at once." A reader who started a rebuild knows why; a false
  "your enclosure is overheating" would be worse than the eight true per-drive alerts it
  is meant to explain, and the way to avoid one is not to make the claim.

  Three consequences of that admission. **The signal is temperature only** — it is the
  thing an enclosure shares, while sector counts and wear are per-drive by nature, and two
  drives' counters moving together is a coincidence rather than a chassis. **The check is
  a *rise*, not a level**: a group that is uniformly hot but steady is an ambient
  condition, and "correlated *change*" is what the feature is called. And the **alert is
  opt-in per channel** (`optInEvents`), the same mechanism as the scan heartbeat, so an
  install that would find it noisy never receives it — while the dashboard card is always
  on, because a card on a page you opened is not an interruption.

  The bar is two settings (`ADR-023`), because the fleet size this suits spans an order of
  magnitude: **how many drives have to move** (default 4) and **the rise that counts**
  (default 8 °C). Four is nearly all of a workstation and a sixth of a 24-bay NAS, so a big
  enclosure should raise it — the question is "did enough of this group move at once to be
  one cause rather than several", and "enough" grows with the group. A `MinDrives` below
  two and a non-positive delta are refused rather than obeyed, so a typo cannot turn the
  check into "any one drive that moved". Two smaller decisions: the baseline is **the
  previous reading**, which is a scan boundary rather than a time window, because that is
  exactly "what changed since we last looked"; and a drive counts for **each of its tags**,
  the same rule as the grouping (`D9`), so a group never hides a drive. Bench drives are
  excluded (`D11`) — a drive being tested is *expected* to heat up. The per-drive alerts are
  deliberately **not** suppressed: each one is true, and silencing a true finding to reduce
  volume is how a monitor becomes one you cannot trust. What `A25` removes is the
  confusion, not the messages.
- **The projection window is too short for a NAS** (`A26`). Retention is configurable;
  the projection window is fixed at 180 days. An owner who raises retention to five years
  is doing so precisely to watch a slow trend — and the app then discards it. The tension
  was flagged when `A16` was built; a NAS is the case that resolves it. **Built**: the
  window is now the `retention_days` setting itself, so what the user keeps is what the app
  reads. It is one number rather than two, and the same one the pruning uses.
- **"Replace first" is the question a fleet asks** (`A24`). The verdict says how a drive
  looks *now*; the ranking says which one to buy a replacement for this month, combining
  verdict, direction (`A14`) and remaining life (`A16`), with the reasoning shown. **Built**
  as **bands** — replace now / soon / watch — rather than a weighted sum, and that is the
  whole design. A weighted sum would be a number nobody could argue with or explain, and
  the point of the ranking is that it shows its reasons: "failing now", "7 new sector
  problems in 30 days", "94% of its rated write endurance used". A reader can disagree with
  a band and act on it; they cannot do either with an unexplained 63.4. A disconnected drive
  is deliberately left out: it is a finding of its own, and ranking it would put a backup
  drive someone unplugged on purpose above a drive that is actually failing. The card can be **turned
  off** (`show_replace_first`, on the Appearance page): it sits above the fleet, so it is the first
  thing on the page whether or not the reader wants it today. The ranking is computed and sent either
  way — this hides a card, not a feature.
- **Bench work is the second use case** (`D11`). Testing a batch of drives you do not want
  in your permanent fleet: label them, test them, export the reports, forget them. **Built**,
  and the design is that **the tag *is* the session** — no mode, no new column, no new
  control. The feature's own words are "label, test and report a batch", and a tag already
  does all three: it groups (`D9`), it survives an unplug, and it exports and imports
  (`C7`). What the tag *means* is the app's contract with the user, so it is named once
  (`store.BenchTag`) and **the server decides** which drives are on the bench — sent per
  drive as `on_bench` and once as `bench_tag` — so the UI never has to know the name or
  split a tag string the way the grouping does.

  What the tag changes is **the aggregates, not the list**: a bench drive is left out of the
  health counts and out of the replace-first ranking, and stays listed, searchable and in
  every export. Being out of the summary is not the same as being hidden — and without the
  exclusion a batch of five drives you already expect to fail would dominate the ranking
  above the drive in service that is actually failing. The exclusion is **stated rather than
  silent** ("6 are tagged bench and left out of those counts"), because a reader who adds the
  counts up and gets a number smaller than the list is owed the reason; that is the same rule
  as the disconnected toggle and the group-count note.

  **Getting a drive onto the bench is a checkbox, not a tag you have to know to type.** The tag is
  the mechanism; "being tested" is the concept, and the first pass found the mechanism was the only
  thing on offer — you had to know the word `bench` to find the feature at all. The device's Notes
  dialog now carries **Being tested — left out of the fleet counts**, kept in step with the tags field
  both ways, and it is there because that is the dialog that already describes the drive, so it is
  where somebody would look. It is deliberately *not* on the dashboard: a card explaining a tag you
  have never used is a card explaining nothing. The card itself now says what the state *means*
  rather than what the tag is called — a reader who wants to know how a drive got there will find it
  in that drive's own tags.

  The batch's verbs — **Test all**, **Export batch**, **Forget all** — live on one card that
  appears only when the bench is non-empty, the same rule as the replace-first shortlist.
  Not on tag-group headings, which are drawn two different ways (card headings and a table
  separator row), and putting three buttons in a table cell to keep the two views level is
  worse than having one place for them. Two smaller decisions: the batch forget is a **client
  loop**, not an endpoint, because a request that deletes every drive matching a tag is one
  mis-click from clearing the fleet and a loop can report partial failure honestly; and the
  export takes **`?tag=`**, so the batch gets its own sheet — a *deliberate* scope, which is
  the only reason it can exist beside the rule that the export ignores the dashboard's
  filter, since view state the caller did not ask for is a different thing from a parameter
  they did.
- **Evidence is cheap** (`A27`). The report export already exists; saying on it that it is
  warranty/RMA evidence costs almost nothing and is the whole reason some people run a
  scan at all. It turned out to be worth more than the sentence: the default
  text format had no attribute table at all, so a report could say `BAD` and name no
  reason a support engineer could check. Every format now names **the attributes that fail
  the drive's *own* manufacturer threshold** — the limit the vendor wrote into the drive,
  in the drive's own terms — and **the counters this app reads as evidence**, from the same
  list the verdict uses, so a report cannot cite a different set of attributes than the
  assessment did. Selecting by the drive's threshold rather than ours is what keeps the
  report from contradicting itself: it is a listing, not a verdict, so a threshold the user
  changed since the scan cannot make the two disagree. It claims evidence, not proof — a
  S.M.A.R.T. report is the drive's own account, and whether that is a warranty case is the
  vendor's call.
- **The drive leads, the app closes** (`A8`, `A27`). The purpose and the provenance were
  written *around* the content — three lines about the document above the drive's identity,
  two about where the numbers came from below the last table — which put roughly eighty words
  of prose about the app around the drive data the file exists to carry. Both facts were worth
  keeping and the sandwich was not. They now sit together at the foot of every format, under a
  rule in the text report and in the footer of the page, at half the length: the reader opened
  the file for the drive, and what it is for is what they want once they have read it. The
  spreadsheet follows the same order, because a `Purpose` row above the identity reads as a
  heading for the sheet rather than a footnote to it. `TestTheReportLeadsWithTheDrive` pins it,
  since "the report says what it is for" is satisfied by putting that sentence anywhere at all.
- **One file for the whole fleet** (`A23`). Two formats, because they answer different
  questions and neither is the other's fallback: a **CSV** with one row per drive, which is
  what you sort when planning a purchase, and a **printable page**, which is what you attach
  when several drives are going back at once. Both render from the same column list and the
  same rows the dashboard shows, through the same ranking, so the export cannot disagree
  with the screen. Three decisions. **A missing reading is an empty cell, never a zero** —
  the em-dash rule from the UI, and it has teeth here because this is the file the numbers
  get aggregated in: a blank is what a spreadsheet's `AVERAGE` skips, while a `0` drags every
  average down and reads as a measurement. **The export is never filtered** — not by the
  dashboard's filter, not by hidden — because a wrong number gets noticed and a missing row
  does not; `hidden` and `connected` are columns, so the reader can see why a drive was not
  on screen. And **the order is the ranking's own**, because it is the one thing a reader
  cannot recover from the file: sorting the band column alphabetically gives fine, now,
  soon, watch. Disconnected drives are listed but unranked — the ranking excludes them by
  design (`A24`) — and their band is empty rather than `fine`, because `fine` is a claim the
  ranking did not make.

None of these adds idle cost. They are views over data the app already stores, plus one
setting (`A26`).

### A19 — SMART self-test: what was built, and the constraints it was built to

The manual half has landed: a short/long button on the device page, a live progress reading
while a test runs, and an abort. The constraints below were written before anyone built it,
and all of them still hold.

- **Opt-in, and never automatic.** Nothing fires a self-test because the app started,
  because a scan ran, or because a drive appeared. The user presses a button, or
  creates a schedule they explicitly asked for. This is the only feature that writes
  to a drive, and the API enforces it: the test kind is an allow-list of two strings,
  so nothing else can ever reach `smartctl -t`.
- **A running test needs a progress reading.** A long test on a 14 TB drive runs for
  many hours — that is the normal case for the drives this app targets, not an edge
  case. A spinner with no percentage is indistinguishable from a hang, so the UI must
  surface the drive's own progress figure (the ATA self-test polling minutes, or the
  NVMe self-test log's percent-complete) and say so plainly when a drive reports
  none. **Done** — the figure comes from the self-test log's own `remaining_percent`,
  which smartctl sets only while a test runs. A drive that reports no percentage is
  never rendered as a test at 0%; a drive that cannot have its log read is told so
  instead of being offered a button that would fail.
  **Both logs are read now, and the NVMe one is what closed this.** The NVMe self-test log has a
  different shape from the ATA log in the one way that mattered: the test *in progress* is a header
  field rather than a table row, and its progress is a separate number rather than a field on an
  entry. A parser that knew only the ATA log therefore reported "no test running" for a drive that
  was mid-test — and the card, believing it, offered the run buttons again, which the drive refused
  with a bare exit status. Fixed against a real WDC SN730 capture (`nvme-sn730-selftest.json`).
  **A log that cannot be read is not a drive that cannot test itself.** The same SN730 under
  smartctl 7.4 reported "Read Self-test Log failed" and under 7.5, with no hardware change, reads it
  fine — so concluding "this drive cannot run self-tests" from a failed *read* was a confident
  statement about the wrong thing, and it told the reader to give up on a drive that tests itself
  perfectly well. The unreadable case is reported as exactly that (`self_test_log_unread`) — but
  **only when the drive offered no self-test log this app reads**, so a drive reporting a
  `self_test` key this parser ignores is not flagged for it. The card offers the buttons while saying
  that progress cannot be shown. Only a drive
  that *says* it does not implement self-tests is reported as unable to.
- **A long test needs a way out.** Added when the manual half was built. A long test
  runs for hours, so aborting is a first-class action rather than a footnote, and it
  sits behind a confirmation because stopping one cannot be undone.
- **A finished test is worth one rescan.** The log the user is looking at is drawn
  from the last scan, which predates the test, so the card triggers a single rescan
  when a test it was watching ends. It is guarded per card, and it has to be: the
  re-rendered card is drawn from stored logs that still show the test running, so an
  unguarded rescan would poll, rescan, poll, forever.
  **A test the card *started* also counts, even if it never saw it running** — and that was a
  real bug, reported as "I don't see the results of a short test I ran already". A short test on
  a fast drive can be over before the first reading, so a card that waited to *see* a test
  running never asked for the rescan, and the result reached the page only if the user thought to
  press rescan themselves. The card now also shows the last finished result itself rather than
  leaving it only in the log table above: "did the test I ran pass?" is asked at the card, and
  the answer belongs where the question was.
- **It lands last.** Docker device access had to be validated on real hardware first
  (`DEPLOYMENT.md`), because a self-test is the one operation where getting the device
  plumbing wrong actually matters. **That is done** — native NVMe, USB-attached NVMe
  and USB-attached SATA SSD all read correctly.
- **Scheduling is not built yet.** A fleet-wide schedule needs one-drive-at-a-time
  staggering; 24 simultaneous long tests is an I/O meltdown, not a feature. The manual
  button is the primitive that has to exist first.


## B. Alerts & notification

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| B1 | ntfy channel (first-class) | MVP | 💤 | |
| B2 | Generic webhook channel | MVP | 💤 | |
| B3 | Health-transition alerts with squelching | MVP | 💤 | |
| B4 | Temperature alerts | MVP | 💤 | |
| B5 | Device appeared / disappeared alerts | MVP | 💤 | appeared is opt-in |
| B6 | Scheduled-scan failure alerts | MVP | 💤 | |
| B7 | Alert acknowledgement (mute a known issue, keep the verdict) | P2 | 💤 | **done** — per drive, scoped to the verdict at the time, with a note saying why. The ack is *spent* when the verdict moves, so it can never hide a drive getting worse. See `NOTIFICATIONS.md` |
| B8 | Maintenance / silence windows (suppress during e.g. array rebuild) | P2 | 💤 | **done** — a `maintenance_windows` setting, one-off or recurring (daily/weekly), optionally scoped to drives picked from a list; evaluated at dispatch time, so no idle cost |
| B9 | Self-test result notifications (pass/fail) | P2 | 💤 | **done** — one `self_test_result` event, high priority on a failure, deduped on the drive's own result identity. See `NOTIFICATIONS.md` |
| B10 | ntfy action buttons ("Open dashboard"), priority, tags | P2 | 💤 | **done** — priority and tags were already there; the action button needs a `public_url` setting, because the app cannot know its own external address and a wrong link is worse than none |
| B11 | Gotify / Telegram / Discord / SMTP adapters | P2 | 💤 | **done** — each a small `Sender`; the editor is data-driven, so adding another is a data change |
| B12 | Per-channel event subscription UI | P2 | 💤 | **done** — checkboxes per channel, served from the server's event list so the two cannot drift. Extended with a **group scope**: a channel also names *which drives* it is about, so a `tank` channel and a bench channel can both want `health_change` and still never see each other's drives. An empty list is every group, a fleet-level event (failed scan, digest, heartbeat) is never filtered, and `Test` ignores it. See `NOTIFICATIONS.md` |
| B13 | Weekly digest (one summary instead of per-event) | P3 | 💤 | **done** — a schedule with `action: digest`, so it rides the scheduler rather than a second timer. One message naming the fleet, what changed, and what to act on. See `NOTIFICATIONS.md` |

## C. Operations & deployment

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| C1 | Docker image + compose reference | MVP | 💤 | |
| C2 | `/healthz` endpoint for Docker `HEALTHCHECK` | MVP | 💤 | trivial, aids orchestrators |
| C3 | SQLite single-file storage + retention pruning | MVP | 💤 | |
| C4 | Reverse-proxy auth documented (nginx/Caddy) | MVP | 💤 | |
| C5 | ~~Built-in single-user basic auth~~ **Not planned** — auth delegated to a reverse proxy (D23) | — | 💤 | |
| C6 | Prometheus `/metrics` exporter | P3 | ⚠ | **done** — `GET /metrics` in the exposition format, **off by default** (`prometheus_enabled`). Hand-written rather than with the client library: four rules and six metrics do not justify multiplying a one-module dependency tree. See `API.md` |
| C7 | Import/export config, notes & labels (JSON) | P2 | 💤 | **done** — one JSON bundle of labels, settings and schedules. Channels are deliberately excluded, because their config holds tokens. See below |
| C8 | Multi-arch image (amd64 / arm64) | MVP | 💤 | home NAS / Raspberry Pi |
| C9 | Configurable base path for subpath hosting | MVP | 💤 | `SMARTSERVE_BASE_PATH` (`ADR-015`) |

## D. UI / UX

| # | Feature | Tier | Idle | Notes |
|---|---|---|---|---|
| D1 | Dashboard cards + device detail view | MVP | 💤 | |
| D2 | Theme picker: dark / light / auto / custom accent colour | MVP | 💤 | **done** — plus Catppuccin (4 flavours), Dracula, Nord, Gruvbox, Synthwave, Hacker, Solarized, Tokyo Night and One Dark palettes |
| D3 | Responsive mobile layout | MVP | 💤 | |
| D4 | Hide / unhide devices, and forget one entirely | MVP | 💤 | escape hatch for unreadable bridges; **forget** is for the plug-in-once workflow |
| D5 | Filter & sort devices (health, name, transport) | P2 | 💤 | **done** — a sort field with a direction toggle, a transport filter built from the transports actually present, and search that covers the bay. See `UI.md` §7.1 |
| D6 | Side-by-side multi-device comparison | P3 | 💤 | **done** — tick two to six drives in the fleet table and compare their attribute tables, aligned by attribute id, with the rows the drives disagree about and the cells a drive has flagged called out. The alignment is server-side (`GET /api/compare`) because the alignment *is* the feature: two drives of one model report their attributes in whatever order smartctl felt like. See `ADR-027` |
| D7 | Localization (multi-language) | P3 | 💤 | **not built, and deferred** — the cost is extracting every string and keeping every locale in step, which is only worth it for a translator who will maintain the result (`ADR-023`) |
| D8 | Dense fleet table view (one row per drive, sortable by risk) | P2 | 💤 | **done** — a Cards/Table switch on the dashboard; the table carries bay, verdict, score, transport, model, capacity, temperature, age and last scan, with sortable headers. See `UI.md` §7.1 |
| D9 | Device grouping by tag/pool, dashboard grouped | P2 | 💤 | **done** — a "Group by tag" toggle, offered only when something is tagged. A drive with several tags appears under each of them. See below |
| D10 | Bay/slot field (physical location on the card and in the report) | P2 | 💤 | **done** — a free-text `bay` in the device's notes, shown first on the card and in the report. See below |
| D11 | Testing session (label, test and report a batch of bench drives without polluting the fleet) | P3 | 💤 | **done** — the `bench` tag *is* the session. The batch is left out of the health counts and the replace-first ranking, and gets Test all / Export batch / Forget all. See below |

## E. Idle-cost guardrails (rules for accepting any new feature)

These rules exist so feature growth cannot erode the product's core promise:

1. **No periodic device reads** outside an explicit or scheduled scan.
2. **Time-based features sleep to the next event** — never tick on an interval.
   (Schedules, retention — computed as a single `time.Sleep`.) A feature that only
   needs to be consulted *when something else already happened* — maintenance
   windows, squelching — is evaluated at that moment instead, which is cheaper still.
3. **Any scraper-style endpoint** (Prometheus) ships **disabled by default**.
4. **UI freshness is always tied to the last scan** — never to live polling. The
   "last scanned" timestamp is visible on every screen.

   There is exactly one exception, and it is narrow: the self-test card (A19) polls
   the drive every 15 seconds **while a test it started is running and the card is on
   screen**. A self-test is a live operation with no other source of truth — its
   progress exists nowhere the app stores — so tying it to the last scan would mean
   watching a two-hour test through a stale percentage. The poll stops the moment the
   drive reports no test running, and stops when the card leaves the page, so no drive
   is ever read for a panel nobody is looking at. Any *other* proposal to poll is
   still refused by this rule.
5. **Anything that writes to a drive is opt-in and never automatic** — the user
   presses the button, or sets the schedule (A19).

## F. Explicitly rejected (see `SCOPE.md` non-goals)

- Write operations to drives (settings changes, secure erase, formatting).
- RAID-controller SMART pass-through (`-d megaraid`, …).
- udev / hot-plug watcher.
- Host CPU/RAM/network metrics — scope creep away from S.M.A.R.T.
- Filesystem-usage graphs — not S.M.A.R.T.; `lsblk` mountpoints are shown for
  context only.

## G. Drive knowledge base strategy (Phase 3)

The owner correctly flagged that a drive-spec knowledge base implies ongoing upkeep.
The strategy minimises that cost:

- **Base layer — smartmontools' upstream `drivedb.h`.** Already ships inside the
  `smartctl` binary in the image and is actively maintained upstream. It supplies
  model families, known-issue flags, and vendor attribute quirks **for free**, with
  no effort from this project.
- **Curated layer — a small bundled JSON table** (rated TBW, rated workload, release
  year, known firmware advisories) that `drivedb.h` does not carry. Community-
  contributed, versioned in-repo, schema-versioned so the app tolerates data both
  older and newer than itself.
- **Update mechanism — ships with each release.** No runtime network fetch, so the
  app stays offline-capable and adds zero idle cost. A future **opt-in** "refresh
  knowledge base" action (only when pressed, never polling) can address freshness
  without breaking the invariant.
- **Graceful degradation.** A drive absent from the knowledge base still gets the
  full S.M.A.R.T. report — only the extras (rated TBW, advisories) are missing. No
  device is ever hidden or downgraded for lacking KB data.
- **Cadence.** Best-effort quarterly-ish updates tied to releases; contributions via
  pull request.

## H. Tabled (recorded, not planned)

Kept here so a good idea is neither silently dropped nor accidentally rebuilt. Nothing in
this section has a tier or a date.

| # | Feature | Why it is tabled |
|---|---|---|
| H1 | Batch self-test ("test all N drives overnight") | **Built, as the scheduled self-test pass** (`A19`, `ADR-024`). The tabled note said to revisit once `A19` shipped, and the scheduler *is* the batch form: one fire tests every drive that can take one, one drive at a time, in serial order. The remaining difference from "test all N drives overnight" is that the pass is a schedule rather than a button — deliberate, because a pass writes to every drive in the fleet and the only thing that should be able to do that unattended is a schedule the user wrote. |

## I. Sequencing: documentation → implementation → UI polish

Feature work lands in three moves, and the third is not optional:

1. **Documentation.** An idea is written into this catalog (and `SCOPE.md`, plus an ADR
   when it carries a real trade-off) *before* it is built, so it is recorded rather than
   rediscovered.
2. **Implementation**, with the test discipline the rest of the project already carries.
3. **UI spot-cleaning.** Every tier of features adds controls, and an interface that grows
   feature-by-feature without a consolidation pass stops being sleek. After each batch the
   UI is reviewed as a whole — grouping, hierarchy, what to demote into a secondary
   surface — and the result recorded in `UI.md`.

This is a working agreement rather than a phase: it repeats for every batch
(`ADR-019`).

### A21 — The knowledge base: three layers, and no clock

The feature asks for a table of rated endurance and known firmware advisories. Two things shaped what
was built, and both are about the same problem: **this data is published, but not machine-readable.**
It lives in datasheets and support pages, so a curated table is the only honest source — which means
the table has to outlive whoever curated it.

So there are three layers, later winning:

1. **A table bundled with the build**, so the feature works on the first page load with no network and
   no configuration. This is the floor, and it is the reason a refresh failing is survivable.
2. **A copy fetched from a configurable URL**, defaulting to the file in this repository but pointable
   at a fork, a gist, or a file on the LAN.
3. **Your own entries**, typed in Settings.

**The bundled file and the refreshable file are the same file.** One format, one validator, one thing
for a contributor to learn — and it means the data can be maintained by somebody who is not the
person who wrote the app. That was the point.

**Never on a timer.** The refresh is a button in Settings. The project is built on costing nothing
while idle, and a knowledge base that quietly polled a URL would be the first thing to break that —
for data that changes a few times a year. The card shows how old the stored copy is, so pressing it is
an informed decision rather than a hopeful one.

Three decisions inside the data model, each one about *not inventing a number*:

- **Endurance is per capacity.** A 250 GB and a 4 TB drive in one family are rated differently, so the
  entry carries a table keyed by the decimal GB the drive reports. A capacity no bucket is close to
  gets **no figure** rather than a neighbour's: a rating for a different capacity is a number about a
  different drive. Within 10% counts as the bucket; within 2% is exact, so the app can say "600 TBW"
  rather than "about 600 TBW".
- **Matching is a substring on the model or the family**, because model strings are messy —
  `"WDC WD40EFRX-68N32N0"` is a WD40EFRX, and many drives report no family at all. The longest pattern
  wins, so a specific entry beats a general one whatever order the file lists them in. Patterns under
  five characters are **refused**: a wrong entry is worse than a missing one, because it carries a
  number that looks authoritative.
- **`matches` covers a series.** The SMR Red line is `WD20EFAX` through `WD80EFAX` — four characters at
  the end, too short to match on safely and impossible to express as one family string.

**Advisories only appear when the firmware matches.** A drive that reported no firmware cannot match a
firmware-specific advisory, because that would be guessing at the one fact the advisory is about. An
advisory with no firmware at all is family-wide, which is how the SMR Red entry is written: the
problem is the model, not a revision.

**Every figure carries its source**, and the app shows it beside the number. A figure nobody can check
is a figure nobody should believe — and a curated table is exactly the kind of thing that acquires
confident-looking numbers from nowhere.

**The knowledge base changes no verdict.** It is context: a denominator for the wear figure, and a
card when something is known to be wrong. The drive's own reported data is what the app reasons from,
and letting a hand-edited table move a verdict would make the verdict unaccountable.

**The shipped table is deliberately short** — Samsung's consumer SATA and NVMe lines, the Crucial
MX500, WD Red/Red Plus/Red Pro/Black, Seagate IronWolf/IronWolf Pro/Barracuda. An entry nobody can
verify is worse than a missing one, because a missing one says so. The one advisory in it is the one a
NAS owner most needs: the SMR Red models, which WD sold as RAID-ready and which are a poor choice in
an array.

**Your entries travel in the config export** (`C7`); the fetched copy does not. One is your data, and
a host migration that dropped it would drop the only copy. The other is a cache of a URL the settings
already carry, and a bundle holding a stale copy of somebody else's file would be worse than
re-fetching it.

