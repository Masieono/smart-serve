# Smart-Serve — manual test plan

`go test ./...` and `node --test internal/webui/jstest/*.test.mjs` cover the rules. This plan covers
what they cannot: whether the right thing appears at all, whether it says the right thing, and whether
it is where you would look for it.

**Every test is a numbered procedure.** Do the steps, compare against *Expect*. Anything that does not
match is a finding — quote its number, and the procedure is the bug report.

The passes build on each other, so **run Pass 0 first**. A large part of this UI is *conditional*, and
a card that is correctly absent looks exactly like a card that is broken; Pass 0 makes the conditions
true so the rest of the plan has something to look at.

## Pass 0 — build the test data

| # | Do | Expect |
|---|---|---|
| 0.1 | Press **Scan now**. | One card per drive, each with a verdict |
| 0.2 | Open a drive → **Notes**. Set **Name** and **Bay** ("bay 4"). Save. | The card leads with the bay |
| 0.3 | In the same dialog, tick **Being tested — left out of the fleet counts**. Save. | A **Testing session** card appears on the dashboard, and the drive leaves the health counts |
| 0.4 | Untick it. Save. | The card goes, and the counts return |
| 0.5 | On two drives, set **Tags** to `tank, spare` and `tank`. Save both. | A **Group by tag** control appears |
| 0.6 | Tick **Being tested** on one drive again. | The batch card is back, and so is **Bench only** |
| 0.7 | Add a **notification channel** (ntfy is the quickest). Send a test. | The channel is listed and the test arrives |
| 0.8 | Create a schedule on a short interval (every 5 minutes). | It runs, and **Last run** fills in |
| 0.9 | Let an hour pass, then scan again. | A **Trend** card and a **Since the last scan** card appear on a drive page |

Two conditions Pass 0 cannot create, and the passes that need them say so: a drive at **caution** or
**bad** (acknowledgement, the replace-first shortlist, a digest at medium priority), and a
**disconnected** drive (unplug one). Keep a caution drive if you have one — it is the trigger for
several cards, and `docs/SMART_INTERPRETATION.md` §4 has the thresholds to make one temporarily.

## Pass 1 — the dashboard

| # | Do | Expect |
|---|---|---|
| 1.1 | Click a **health count** (e.g. "2 healthy"). | The grid filters to those drives, and the count is highlighted |
| 1.2 | Open a drive, then go back. | The filter is still applied |
| 1.3 | Type `bay 4` into **Filter**. | Only that drive is listed |
| 1.4 | Press `/`. | The filter box takes focus |
| 1.5 | Set **Sort** to Capacity. | Biggest first, and the direction reads **Descending** |
| 1.6 | Press the direction button. | The order reverses, and both its word and its arrow change |
| 1.7 | Set **Sort** to Name. | A→Z, and the direction resets to **Ascending** without you touching it |
| 1.8 | Set **Transport** to a value, then back to **All**. | Only that transport is listed; All restores |
| 1.9 | Press **Table**. | One row per drive, and the switch shows Table pressed |
| 1.10 | **Hard-refresh** the page (Ctrl-Shift-R). | Still the table |
| 1.11 | Press **Group by tag**. | A heading per tag; untagged last; a multi-tag drive under each, with the note about the counts |
| 1.12 | Press **Export…**. | A dialog with **Format** and **Drives**. Choose CSV, then Export — a file downloads |
| 1.13 | Press **Export…** again and choose **Only tank**. | A file named for the tag, with only those drives |
| 1.14 | Press **Bench only**. | Only the batch is listed |
| 1.15 | Press **Show N disconnected** (needs an unplugged drive). | The missing drives appear, dimmed |
| 1.16 | Rescan a drive from its card. | A toast, the card updates, and **the page does not jump to the top** |

Cards that only appear under a condition: **Replace first** (a drive that qualifies), **Heating
together** (4+ drives in one tag group rising together between two readings), **Testing session** and
**Bench only** (a drive ticked **Being tested**).

## Pass 2 — one drive's page

| # | Do | Expect |
|---|---|---|
| 2.1 | Open a healthy drive. | Verdict, score, grade, and the reasons behind them |
| 2.2 | Read the **Outlook** block. | One line of caveat, not a paragraph |
| 2.3 | Change the **Trend** window 30 → 7 → All. | The plots change, the axis relabels, and **the page does not move** |
| 2.4 | Hover a trend line, then focus one and press ←/→. | The readout follows the pointer and the keys; the marker sits on a reading |
| 2.5 | Pick a date on the axis and follow it up through the plots. | A faint vertical rule runs through every plot at that date |
| 2.6 | Scroll to **Drive logs**. | Both counts, and a self-test table when the drive has one — each row dated, with the drive's own lifetime hours beside the date |
| 2.7 | Check the **attribute table**. | `?` explains each attribute; vendor rows are named, not `Unknown_Attribute` |
| 2.8 | Press **Show raw output**. | smartctl's own document |
| 2.9 | Press **Export**, once per format. | Four files, each in the format asked for |
| 2.10 | Press **Notes**, change the name, **Save**. | The header updates. Press **Save** a second time — the button must come back |
| 2.11 | On a caution drive: **Acknowledge**, then let the verdict move. | The ack is spent when the verdict changes, and the drive alerts again |

Cards that only appear under a condition: **Knowledge base** (a drive the table knows — see
`KNOWLEDGE.md`), **Since the last scan** and **Trend** (two scans), **Acknowledge** (caution or bad),
the no-S.M.A.R.T. notice (a drive that reports none, or a blocked read).

## Pass 3 — self-tests

The only part of the app that writes to a drive, and the only part that polls one. Do this pass on a
spare drive if you have one.

| # | Do | Expect |
|---|---|---|
| 3.1 | On a SATA drive, press **Short test** and confirm. | A toast, then a percentage left and a **Stop test** button |
| 3.2 | Wait for it to finish (a minute or two). | **Last test** appears with the drive's own verdict |
| 3.3 | Reload the page. | **Last test** is still there, and the **Drive logs** table has the entry |
| 3.4 | Start a short test, then press **Stop test** mid-run and confirm. | The test stops and the card returns to idle |
| 3.5 | On an **NVMe** drive, press **Short test** and confirm. | Progress and a **Stop test** button. Both logs are read now, so NVMe behaves like ATA |
| 3.6 | While one is running, look at the card. | It offers **Stop test**, not the run buttons — so there is nothing to press twice and nothing for the drive to refuse |
| 3.7 | Press **Long test** on a drive that refuses one. | The card says the drive refused it **and greys the button out** — never a bare "smartctl exit status 4" |
| 3.8 | After 3.7, look at **Short test**. | Still usable — a refused long test says nothing about the short one |
| 3.9 | Open a drive that cannot self-test. | It says so, with no buttons offered |
| 3.10 | Start a test, navigate away, come back. | Progress picks up from the drive rather than from a clock |
| 3.11 | Watch the network tab while a test runs, then navigate away. | A request every 15s while the card is on screen; **none** once it is not |

**A drive whose self-test log cannot be read** says so and still offers the buttons: a log that failed
to read is not a drive that failed to test. smartctl 7.4 could not read the SN730's NVMe self-test log
and 7.5 can, with no hardware change — which is why the two are reported separately.

## Pass 4 — schedules and digests

| # | Do | Expect |
|---|---|---|
| 4.1 | **New schedule** → Repeat: every few minutes. Save. | It appears, with a next run |
| 4.2 | Press **Run now**. | A scan starts |
| 4.3 | Edit it and switch Repeat to "On chosen days, at a time". | The time and day controls appear |
| 4.4 | Switch Repeat to **Cron expression** and type `0 3 * * *`. | A plain-English preview of when that is |
| 4.5 | Type nonsense into the cron field. | An error in the field, not after a round trip |
| 4.6 | Set **Action** to "Send a digest of the period". | A hint appears saying **where the digest goes** |
| 4.7 | Set it back to scan. | The hint disappears |
| 4.8 | Save, then press **Save** again in the same dialog. | The button must come back |
| 4.9 | Wait for a digest to fire. | It arrives at your channel, and the delivery log records it |
| 4.10 | Set **Action** to "Run a self-test on each drive". | A **Test** picker appears (short / long), and the hint says it runs one drive at a time and writes to the drives |
| 4.11 | Set it back to scan. | Both the picker and the hint disappear |
| 4.12 | Save it with a short interval (every 5 minutes) and **Test** = short. | The list shows **Self-test · short** in the Action column |
| 4.13 | Press **Run now**, then look at the schedules page. | A notice above the table: "Self-tests running — 0 of N drives done", naming the drive it is testing |
| 4.14 | Watch two drives, or one, while the pass runs. | **Only one drive is testing at a time**, and the next starts only after the previous one finishes |
| 4.15 | Open the drive that is testing. | Its self-test card shows the test in progress, with **Stop test** |
| 4.16 | Let a drive's test finish. | The drive is re-read on its own — the result appears without a scan, and `self_test_result` notifies if a channel subscribes |
| 4.17 | Press **Run now** again while the pass is still draining. | The fire is skipped, and the app log says a pass is still running — no second pass |
| 4.18 | Restart the app mid-pass. | The pass stops; the drive that was testing keeps testing. The next fire skips a drive already testing itself |
| 4.19 | Add `- /mnt:/mnt:ro` to the compose file and restart. | The **Partitions** card's usage column fills in for filesystems under `/mnt` |
| 4.20 | Open a drive with a partition table. | One row per partition: label first, node name under it, size, filesystem, mountpoint, usage, type |
| 4.21 | Press **UUID** on a partition row. | "UUID copied" — and the value pastes as what `blkid` reports for it |
| 4.22 | Press **UUID** on a partition holding a volume group. | It copies the *partition* UUID, since that partition has no filesystem of its own |
| 4.23 | Open **Details** under the partition table. | Logical and physical sector size, 512e or 4Kn, TRIM support, partition table type |
| 4.24 | Uncheck **Partitions on the drive page** in Settings and reopen a drive. | No Partitions card at all — not an empty one |
| 4.25 | Uncheck **Storage usage**, reopen a drive. | The card is there and the Used column is **gone**, not blank |
| 4.26 | Look at a partition that is not mounted (a volume-group member). | A dash in Used — **not** "not visible", which is for a filesystem the container cannot see |
| 4.27 | Look at the Used column. | A **bar** with the percentage, not a bare number |
| 4.28 | Open a drive whose filesystem the container can see, and check the fleet card. | A storage bar under the card's facts, with the mountpoint it belongs to |
| 4.29 | Compare the fleet card's storage figure with the drive page's. | Identical — both come from the server's "fullest filesystem" |
| 4.30 | Open the drive page's Usage and media card. | **Storage** leads the card as a bar, with the filesystem named underneath |
| 4.31 | Look at a partition holding a volume group (no filesystem). | "Linux LVM" under the dash in the Filesystem cell, not in a column of its own |
| 4.32 | Open **Details** under the partition table. | Sector sizes, TRIM, partition table type, **approximate** unallocated space, and the outside-container explanation if any |
| 4.33 | Scan the same drive twice, a day apart. | The trend card gains a **Storage** row, with the delta in *points* |
| 4.34 | Check the partition card heading. | A count and nothing else — no table type, no unallocated figure |
| 4.35 | Open Settings. | An index of closed sections, each with a hint — not a wall of controls |
| 4.36 | Open **Appearance**, change the theme. | The section stays open across the re-render |
| 4.37 | Paste nonsense into the per-model editor and press Save. | The error is visible — the section forces itself open |
| 4.38 | Open Settings, navigate away, come back. | Sections are closed again — the state is per-visit, not stored |
| 4.39 | Open **Settings** in the sidebar. | Four sub-entries appear under it: Thresholds, Notifications, Appearance, Data |
| 4.40 | Click each one. | Its own short page, with the section name as the heading — no collapsing, nothing hidden |
| 4.41 | Change a threshold and try to navigate away. | "Leave without saving?" — Cancel stays, Discard leaves |
| 4.42 | Change a threshold, press Save, then navigate away. | No prompt — there is nothing unsaved to lose |
| 4.43 | Look at Save before touching anything. | Disabled — there is nothing to save |
| 4.44 | Open `/settings/thresholds` directly in a fresh tab. | That page, not the default one — the URL is the page |
| 4.45 | Open `/settings` and `/settings/nonsense`. | Both land on Thresholds rather than on a blank page |
| 4.46 | Open **Thresholds** and look at the bottom row. | "Storage warning level", 90% by default |
| 4.47 | Set it to 50 and Save. | Usage bars at or above 50% are amber on the fleet card and the drive page |
| 4.48 | Check the verdict badge of a 95%-full drive. | Unchanged — the level colours a bar, it does not move the verdict |
| 4.49 | Open a drive pulled from a RAID array. | "a member of a Linux md array (`linux_raid_member`)" — not "unpartitioned or could not be read" |
| 4.50 | Open a disk with a filesystem written straight to it (no table). | "holds an `ext4` filesystem written directly to the disk" |
| 4.51 | Open a disk that has genuinely never been set up. | The old message — the app must not claim to know more than it does |
| 4.52 | Open a drive with a partition table. | A **map** above the table: one segment per partition, sized by share of the disk |
| 4.53 | Point at a segment. | Its label, size, usage and mountpoint |
| 4.54 | Look at a disk with free space. | A hatched free segment, whose tooltip says the figure is approximate |
| 4.55 | Look at a drive whose partitions leave only an alignment remainder (a few MB). | **No** free segment — the background shows through instead |
| 4.56 | Open a drive with a volume group on it. | The logical volume is **not** drawn as its own segment; it is a row in the table only |
| 4.57 | Open a drive whose filesystem the container cannot see. | Its segment is hatched, not filled at 0% |
| 4.58 | Turn **Storage usage** off in Settings → Appearance. | The map goes with the usage column |
| 4.59 | Start a long test on one drive, then go back to the fleet. | That drive's card carries a **Testing** badge — no need to open each page to find it |
| 4.60 | Press Run on a drive whose card already shows Testing. | "This drive is already running a self-test" — but you should not have had to ask |
| 4.61 | Let a test finish, then scan. | The badge is gone; the result is in the drive's self-test log |
| 4.62 | Open a drive testing itself. | Its card reads the drive live, so the figure is fresher than the fleet badge's |
| 4.63 | Look at the Self-test card on a drive with past tests. | The **self-test log** is in this card, under the buttons — not in the Drive logs card |
| 4.64 | Start a test and watch the card. | The progress bar and the history are separated by a rule; nothing from a past test sits beside the running one |
| 4.65 | Open a drive with no self-test history. | The card is the buttons and the note — no empty log table |
| 4.66 | Start a test on a drive whose firmware leaves a percentage in an aborted log row. | **Not** stuck at that figure — the drive's own status field wins, and the test ends when the drive says it did |
| 4.67 | Press Stop on a test behind a bridge that ignores the abort. | "The drive accepted the abort but is still reporting a test in progress" — not a claim that it stopped |
| 4.68 | Start a long test on one drive and watch the fleet. | The badge's percentage **moves**, without opening the drive's page |
| 4.69 | Point at a segment of the disk map. | The label appears **at once** (no browser tooltip delay), and the matching table row is marked |
| 4.70 | Open a drive whose last scan is 4 hours old (`device_refresh_minutes` = 15). | "4 hours ago — reading the drive now…", then the page redraws with fresh data |
| 4.71 | Set `device_refresh_minutes` to 0 and reopen it. | The age is still shown; nothing is read |
| 4.72 | Open a drive that has never been scanned. | It is read, whatever the window says — otherwise the page is empty for no reason |
| 4.73 | **Pull a disk from a RAID array and open it.** | "a member of a Linux md array (`linux_raid_member`)" — **UNVERIFIED**, see the caveat in `FEATURES.md` §A28 |
| 4.74 | Open a drive that reports `ata_smart_selective_self_test_log` (most modern ATA drives do). | **No** "this drive's self-test log could not be read" notice — its results are on the page. The notice is for drives with no readable self-test log at all |
| 4.75 | Start a short test on that drive and leave the page alone. | The bar clears **by itself** when the drive finishes. If it needs **Scan now**, the live read was skipped — see `TESTING.md` §8.8 |
| 4.76 | Open a drive whose read always fails (the Pi's SD card, or a card reader with no card). | The age line says it could not be read, and the scan history gains **one** failed read — not five in a second. Reopening after the window elapses gives one more |
| 4.77 | Scroll to the bottom of a device page, then click **Devices** in the sidebar (or the wordmark). | The fleet opens **at the top** — no scrolling back up by hand. Press **Scan now** instead, and your place is kept |

## Pass 5 — notifications

| # | Do | Expect |
|---|---|---|
| 5.1 | **New channel**, and open the type list. | ntfy, Gotify, Telegram, Discord, email, webhook |
| 5.2 | Fill one in, then press **Test** three times. | Three test messages, and the button usable each time |
| 5.3 | Press **Save**, then **Save** again. | The button must come back |
| 5.4 | Press **Test** on the saved channel in the list. | A test arrives |
| 5.5 | Tick some events and save, then untick them. | The chips on the card follow |
| 5.5a | Tag two drives `tank` and one `bench` on the dashboard, then open a channel and look at **Only drives in these groups**. | One checkbox per group in use, none ticked |
| 5.5b | Tick `tank`, save, and look at the card. | **Only drives tagged** · `tank` — and the events line is unchanged |
| 5.5c | Reopen the editor without changing anything and save again. | `tank` is still ticked — the scope must not be quietly widened |
| 5.5d | Untick `tank` and save. | The card is back to **Every group** |
| 5.5e | Press **Test** on the scoped channel. | A test arrives: the scope must not make a channel untestable |
| 5.6 | **Add** a maintenance window: one-off, now to +10 minutes. | It is listed and marked active |
| 5.7 | Add a daily window and a weekly one. | Both listed, with their schedules in words |
| 5.8 | Scroll to the **delivery log**. | Every test above is recorded there |

The maintenance card is absent when there are no windows — that is the design, not a failure.

## Pass 6 — settings and the knowledge base

| # | Do | Expect |
|---|---|---|
| 6.1 | Change a threshold and press **Save settings**. | A toast. Press **Save** again — the button must come back |
| 6.2 | Switch the temperature unit with an unsaved edit in a threshold field. | The number converts, and nothing typed is reinterpreted |
| 6.3 | Pick a theme. | It applies at once, and survives a reload |
| 6.4 | Press **Refresh now** on the knowledge base card. | A toast, the status line updates, and **the page does not jump** |
| 6.5 | Press **Download the shipped table**. | A JSON file |
| 6.6 | Paste `{"nope": 1}` into **Your own entries** and press Save. | The error is shown **beside the box**, naming the field |
| 6.7 | Press **Discard the fetched copy** when one exists. | The status line drops back to the shipped count |
| 6.8 | Press **Export** in Import and export. | A JSON bundle downloads |
| 6.9 | Press **Import** and choose that file. | A confirmation first, then a summary of what was applied |
| 6.10 | Scroll to **Per-model thresholds** and press **Load the fleet's models**. | One entry per model in the fleet, at the values on the sliders above |
| 6.11 | Change one number in one entry and press **Save settings**. | A toast. Reload — the change is still there |
| 6.12 | Press **Load the fleet's models** again. | The entry you edited is **kept**, not overwritten |
| 6.13 | Type a threshold name that does not exist (`"reallocated_bad"`) and save. | An error **beside the box** naming the key, and nothing saved |
| 6.14 | Change one model's temperature threshold, save, and **Scan now**. | That model is judged by its own number; the other drives are unchanged |

## Pass 8 — arm64 (Raspberry Pi)

**Status: partial hardware verification, updated 2026-11-08.** Smart-Serve is running on a
Raspberry Pi 5 with DietPi. The operator previously reported building, starting and scanning;
the supplied SSH outputs confirm native arm64 execution, HTTP health, device-access configuration,
USB SAT discovery and a successful full smartctl read of the attached WDC disk. The operator has
also completed an app scan and verified its displayed identity, attribute count and selected raw
values. A short test completed without error, but the automatic UI completion check **failed**:
the page stayed at `90% left` after waiting and reloading. The live API reported completion,
while the captured saved record contained `remaining: 90` and an unread-log flag, matching the
source path that skips a live refresh. **That path is fixed** (see 8.8): the flag was raised for a
log the app *does* read, and the card no longer skips its live read. The operator then clicked
**Scan now**: the progress bar cleared, short/long test buttons returned, and the successful history
entry appeared and persisted after reload — so **manual recovery and completed-history display
passed**, and the automatic-completion check needs a retest against the fixed build. A similar
symptom is reported on amd64, so an ARM-specific fault is not established. The SD card is visible in
the app, but opening its page caused flicker and repeated `scan complete: 0/1 readable` toasts: its
no-repeat-scan check **failed**. The API confirms `last_scan: null`, a device-type detection error
and five failed device reads in the same displayed second, matching the retry path in source.
**That loop is fixed too** (see 8.9): at most one attempt per refresh window, per drive. Returning
to the dashboard stops the repeated scans, which remains the workaround for any build without the
fix. Both defects need a retest on this hardware; the next check compares dashboard health and
disconnected-device display with the API. Other checks and parity remain open.
CI builds both architectures but does not run the application against ARM hardware.

This section is the ARM verification record; setup is in
[DEPLOYMENT.md](DEPLOYMENT.md#a-raspberry-pi-as-a-development-host).
Work through **one test at a time**, starting with the next step below. Keep the deployed image
and configuration fixed for the baseline. Record each result before moving on; untested is not passed.

### Recorded environment — 2026-11-08

Evidence: operator-supplied SSH transcripts and a subsequent UI verification report. These checks
were performed on the Pi deployment, not by CI.

| Item | Observed |
|---|---|
| Host | Raspberry Pi 5 / DietPi; `uname -m`: `aarch64`; `getconf LONG_BIT`: `64` |
| Kernel reported by smartctl | `6.18.50+rpt-rpi-2712` |
| Docker server / image architecture | `linux/aarch64` / `linux/arm64` |
| Container | `smart-serve-dev` |
| Local image ID | `sha256:6a2490cfed98e3695e0c25df83a47b84a561608f6efa6ac55a0fb1cd8b9bd528` |
| Application-reported version | `v1.1.0-23-g949fbd6` — exact source revision not independently checked |
| Bundled tools | smartctl **7.5 r5714**, built for `aarch64-alpine-linux-musl`; util-linux / lsblk **2.42.3** |
| Device access | `CAP_SYS_ADMIN`, `CAP_SYS_RAWIO`, `a *:* rwm`, and `/dev:/dev:ro` |
| Data storage | Named volume `smart-serve_smartserve-dev-data` mounted read-write at `/data` |
| Restart policy | `no`, matching development Compose; automatic recovery not tested |

The image ID identifies the tested local artifact, not a GHCR manifest digest. This session
does not validate pulling or running a published release. The version alone does not establish
the source revision: `update.sh` currently computes it before pulling updates.

The bundled tool versions are the Alpine packages of *that* build, not pins in the repository —
`Dockerfile` installs `smartmontools` and `util-linux` unpinned, so a later build can carry different
ones. Anything that depends on a smartctl version (log shapes, exit codes, whether a drive answers at
all) should be checked against the image actually running rather than against the numbers above.

### Test results and remaining checks

Existing test numbers are retained so subsequent results can refer to them.

| # | Verify | Status from supplied output and operator reports |
|---|---|---|
| 8.1 | Native arm64 deployment starts and reports its version. | **Execution verified.** Architecture and executable version commands succeeded. Build execution and source-to-version correspondence were not captured. |
| 8.2 | Device-access setup, startup probe and HTTP health. | **Verified for this setup.** Probe: `device access ok`, `devices=2`, `probed=/dev/sda`; server listening on `:8080`; Docker `healthy`. This probe alone does not verify full SMART reads or every drive. |
| 8.3 | Container SMART read and app scan, identity and attribute-table checks. | **Passed for this disk/setup.** The container read returned exit 0. The operator completed an app scan and confirmed the WDC model, matching serial, 17 attributes and raw values 0 for IDs 5, 197 and 198, with no apparent errors or missing values. Other displayed fields were not individually compared. |
| 8.4 | USB transport and any required override. | **SAT discovery, explicit SAT read and app scan verified for current settings.** `--scan-open` detected `sat` / `ATA`; the full read succeeded with `-d sat`. The app also reads the disk successfully. Its override setting was not reported, so automatic-vs-manual transport selection is not established. Override controls, bridge chipset and any second bay remain unverified. |
| 8.5 | Ten-minute idle CPU/RSS measurement with no browser, scan or self-test active. | **Single sample recorded; idle acceptance pending.** CPU `0.04%`; process RSS/high-water mark `22,496 kB` (about **21.97 MiB**); 11 threads; 16 file descriptors; restart count 0; `OOMKilled=false`. Browser/workload conditions were not captured. |
| 8.6 | NVMe temperature, health fields and comparison with amd64. | **Not tested.** No NVMe device appears in this capture. |
| 8.7 | Overnight scheduled scans and stable memory/file-descriptor usage. | **Not tested.** Startup and later log timestamps alone do not establish a controlled stability run. |
| 8.8 | App-triggered short self-test: start, progress, completion and result after page reload. | **Drive completion passed; UI running-state check failed, and the cause is now fixed — retest needed.** Direct smartctl output shows execution status 0 and the newest short test `Completed without error` at 45,446 hours. The operator confirmed the app continues showing `90% left` and only **Stop test** after extended waiting and manual reload. The live API confirmed completion (`passed: true`, no `running`) but also set `self_test_log_unread` for the selective-test log; the saved record from 16:02:49 UTC (scan 18) carried that flag and `remaining: 90`. **Fixed:** the flag was raised for a log the app reads (see the 8.8 fix note below), and the card no longer skips its live read on mount. **Manual recovery and completed-log visibility passed:** after **Scan now**, the progress bar disappeared, short/long buttons returned and the successful short offline entry was visible; these results survived a page reload. Exact duration remains unverified, and the fixed build has not been run on this hardware. A similar symptom is reported on amd64. |
| 8.9 | SD-card discovery in the app, unsupported-SMART notice and absence of repeated automatic scans. | **App visibility verified; no-repeat-scan check failed, and the cause is now fixed — retest needed.** The API reports `last_scan: null`, `smart_available: false`, health/cause `unknown`, and `Unable to detect device type`. Scan IDs 175–179 each record one failed device read at 22:09:03 UTC. The operator confirms the repeated scans occur on the SD-card page and stop on returning to the dashboard. **Fixed:** the staleness re-read is now bounded to one attempt per refresh window, per drive (see the 8.9 fix note below). Dashboard health rendering and the detail-page notice remain unverified; the fixed build has not been tested on amd64. |
| 8.10 | Disconnected-device inventory and dashboard filter. | **API state captured; UI check pending.** Three historical records have `is_missing: true`; the SD card and tested 2 TB WDC disk have `is_missing: false`. This records API state, not independent verification of physical attachment or a hotplug/reconnect test. |

**Observations to retain for follow-up:**

- Host `lsblk` identifies the 2,000,398,934,016-byte USB disk as `WDC WD20EURX-64HYZY0`
  with a serial; container `lsblk` reports `Dual SATA Bridge` with no serial. Capacity and
  device node agree. The follow-up smartctl read returns the WDC model and the same serial as
  host `lsblk`. The operator subsequently confirmed the matching model and serial in the app,
  resolving the identity-display concern for this disk/setup. Identity across hotplug or other
  enclosure bays remains untested.
- The 31,914,983,424-byte SD card is visible in both environments and its app page can be opened,
  but that page repeatedly scans (8.9 below). Host partition mountpoints are `/boot/firmware`
  and `/`; the container instead sees its own mounts, including `/data` and `/etc/hosts`.
  The latest API storage summary selects `/etc/resolv.conf` at 19% used. Host mountpoint and
  filesystem-usage parity are not verified by this container-visible figure.
- Docker reported memory as `0B / 0B`, which is not a usable memory measurement here; its cause
  remains unverified. The process RSS above comes from `/proc/1/status`. It exceeds the stated
  under-20-MB target at that instant, so do not mark the ARM idle budget as passed. Repeat under
  controlled idle conditions; keep container memory and process RSS separate.
- `scheduler started` and `dispatching notifications` appeared in the logs. Neither proves
  scheduled scans completed or a notification reached its destination.

### Completed — 8.3 container SMART read, 2026-11-08 15:03:16 UTC

The operator ran:

```sh
docker exec smart-serve-dev smartctl -x --json -d sat /dev/sda
```

The complete JSON parsed successfully and reported `smartctl.exit_status: 0`.
This verifies the container's smartctl read for this disk/enclosure; the app's parsing and display
are not exercised by this command.

| Reading | Captured result |
|---|---|
| Identity | `WDC WD20EURX-64HYZY0`; serial matches host `lsblk`; firmware `80.00A80` |
| Capacity | 2,000,398,934,016 bytes (about 2 TB) |
| SMART support / drive-reported status | Available, enabled / passed |
| ATA attributes | 17 rows; raw counts for IDs 5, 197, 198 and 199 are all 0 |
| Temperature / power-on hours / power cycles | 26 °C / 45,445 hours / 150 cycles |
| Extended ATA error log | Count 0 |
| Existing self-test log | One short test: `Aborted by host`, 10% remaining, at 45,422 power-on hours |

The operator subsequently identified the aborted test as one they stopped manually after the UI
remained at `In progress, 90% left` for much longer than five minutes. The abort's origin is now
known. Later captures below identify stale saved progress for the completed attempt; they do not
prove the cause of this earlier aborted attempt. The captured aborted log's 10% remaining and the
reported UI's 90% left were not captured simultaneously, so they do not establish a percentage
conversion bug. This earlier attempt did not verify successful completion.

### Completed — 8.3 app scan and display check, 2026-11-08

The operator completed a scan in Smart-Serve and confirmed:

- Model `WDC WD20EURX-64HYZY0` and serial match the captured smartctl output.
- The SMART table contains 17 attributes; raw values for IDs **5**, **197** and **198** match (0).
- No error notices or other missing values were apparent during this check.

This passes the requested identity and attribute-table checks for the attached disk. Temperature,
power-on hours, the remaining individual fields, exports and history have not been explicitly
compared; a general absence of visible errors does not establish complete UI or amd64 parity.

### Partial result — 8.8 short self-test, 2026-11-08

The operator initially reported that **Short test** started and the page updated every 15 seconds,
but stayed at `In progress, 90% left`, including after manual reload. No elapsed-time estimate
was visible. The earlier manually aborted attempt lasted much longer than five minutes. The
subsequent direct read below establishes a completed short test; the UI state at that later
capture was not separately confirmed.

The same symptom was also observed by the operator on AMD/amd64 systems. Later Pi captures
support a shared application defect involving stale saved progress and a skipped live read.
Matched build, drive/enclosure and live status captures from both architectures are not yet
available, so this is not a confirmed ARM regression.

**What the repository implements:**

- [The self-test UI](../internal/webui/assets/js/views/selftest.js) polls at a default interval of
  15 seconds. The button and confirmation show the drive's duration estimate when available
  (two minutes in this drive's captured data). When a running percentage is returned, the card
  shows that percentage without an elapsed-time estimate. The elapsed-time/estimated-duration
  display is a fallback while awaiting a result without a reported running state; it is not
  shown alongside `90% left` and its start time is held only in the current page.
- [The SMART reader](../internal/smart/selftest.go) uses `smartctl -x --json` for progress reads.
  [The parser](../internal/smart/logs.go) prefers the ATA execution-status field over old log
  percentages when that field is present. These are source findings, not proof of the raw
  status currently returned by the Pi's drive or of the exact code in its deployed image.
- [The background watcher](../internal/scan/selftestwatch.go) can also read drives recorded as
  testing. Closing a browser tab alone does not establish that all app polling has stopped.

The absence of an elapsed-time estimate with a reported percentage matches the implementation.
The drive and live API reported a completed short test while the UI stayed in a running state
until the manual scan recorded below. The API's running/completed interpretation is correct in
the captured response.
Its additional unread-log flag and the UI's handling of that flag are the leading explanation
from source inspection. The saved record below now confirms both stale progress and the flag;
actual browser request behavior and the exact deployed source remain unverified.

### Follow-up — 8.8 drive completion verified, 2026-11-08

The operator supplied the output of:

```sh
docker exec smart-serve-dev smartctl -c -l xselftest,selftest -d sat /dev/sda
```

This reads the execution status/capabilities and extended self-test log, with a standard-log
fallback, without starting or stopping a test. The options are documented in the
[smartctl manual](https://github.com/smartmontools/smartmontools/blob/main/src/smartctl.8.in).

The drive's execution status is **0** (no test running). Its generic status text allows either
completion or no previous test; the newest log entry below establishes successful completion.
This is an execution-status value, not a captured process exit code.

| Log entry | Type | Result | Remaining | Drive lifetime hours |
|---|---|---|---|---|
| 1 (newest) | Short offline | Completed without error | 00% | 45,446 |
| 2 | Short offline | Aborted by host | 50% | 45,446 |
| 3 | Short offline | Aborted by host | 10% | 45,422 |

The operator estimated about ten minutes since starting when this output was captured, without
recording a start time. This is not a measured test duration; completion may have happened earlier.
The drive still advertises a two-minute short-test estimate. Entry 2 is newly visible in this
capture; its abort has not been separately explained. Entry 3 is the earlier operator-aborted test.

**Verified:** a short test completed without error, and its result can be read through the ARM
container. The later automatic UI check below **failed** to clear the running state. A subsequent
manual scan restored the idle controls and made the successful entry visible across reloads.
Exact duration remains unverified. The display symptom is reported on both architectures; these
results do not establish an ARM-specific cause.

### Failed check — 8.8 app state after completion, 2026-11-08

After the direct read confirmed completion, the operator left the device page open for an extended
period and manually reloaded it. Both before and after reload, the app still showed the progress
bar and `90% left`, with **Stop test** as the only available test button. It did not return to idle.
History visibility at that point was not reported; it was later verified after the manual scan below.

This fails the app's running-state/automatic-completion acceptance check on the Pi. Keep drive
completion marked passed and this app check marked failed until a retest verifies it. The same
kind of symptom is reported on amd64. The later API result below narrows the likely cause to
shared app logic; the deployed page's exact failure path is not yet verified.
No application-code change has been made in this verification session.

### Follow-up — 8.8 live API completion verified, 2026-11-08

The operator ran the read-only live-state request:

```sh
docker exec smart-serve-dev wget -qO- \
  'http://127.0.0.1:8080/api/devices/WD-WCC4M5VVF6RY/selftest'
```

The response contains **no top-level `running` field**. Its `logs` contain:

| Field | Captured value |
|---|---|
| `self_test_count` / `error_count` | 3 / 0 |
| `self_test_supported` | `true` |
| `short_minutes` / `extended_minutes` | 2 / 275 |
| Newest `self_test_entries` item | `Short offline`, `Completed without error`, `passed: true`, `hours: 45446` |
| Other entries | Two `Aborted by host` results at 45,446 and 45,422 hours |
| `self_test_log_unread` | `ata_smart_selective_self_test_log` |

All three entries omit `remaining`. The captured live API therefore agrees with the completed
drive test, while also marking a separate log as unread. This rules out an incorrect `running`
value in this particular API response; it does not prove what response the browser received.

**Likely cause from source inspection, supported by the saved-state capture below:**

1. `unreadSelfTestLog` in [logs.go](../internal/smart/logs.go) flags top-level keys containing
   `self_test`, except the recognized ATA and NVMe history logs. This catches
   `ata_smart_selective_self_test_log` even when the ordinary self-test history is parsed correctly,
   as it was in this response.
2. [The device endpoint](../internal/httpapi/devices.go) builds `device.logs` from the saved raw
   scan output. [The device page](../internal/webui/assets/js/views/device.js) gives those saved
   logs to the self-test card. This is a different source from the live `/selftest` response.
3. [The self-test card](../internal/webui/assets/js/views/selftest.js) first displays a saved entry
   with `remaining` as running. If the saved logs have `self_test_log_unread`, it skips the live
   read on page load. That path can preserve an old progress display across reloads, even though
   the live endpoint correctly reports completion. The unread flag also gates the fallback
   progress display when no running state is reported.

These branches have no CPU-architecture condition. The Pi's saved logs below now confirm the
two inputs to this path: stale running progress and the unread-log flag. This supports a shared
application issue, consistent with the operator's amd64 observation. Browser network activity
and the exact deployed source remain unverified; the issue is not fixed. No application code
has been changed.

### Follow-up — 8.8 stale saved progress confirmed, 2026-11-08

The operator captured the saved device record with:

```sh
docker exec smart-serve-dev wget -qO- \
  'http://127.0.0.1:8080/api/devices/WD-WCC4M5VVF6RY'
```

The response identifies the same WDC disk and serial. `device.last_scan`, `device.last_seen`
and `snapshot.ts` are `2026-11-08T16:02:49Z`; `snapshot.scan_id` is **18**.

| Reading | Saved device record | Previously captured live self-test API |
|---|---|---|
| Running progress | First log entry: `status: "in progress, 90% remaining"`, `remaining: 90` | No `running` field; no entry has `remaining` |
| Finished history | Two `Aborted by host` entries at 45,446 and 45,422 hours | Newest entry is `Short offline`, `Completed without error`, `passed: true`, at 45,446 hours, followed by the two aborted entries |
| `self_test_count` | 2 | 3 |
| `self_test_log_unread` | `ata_smart_selective_self_test_log` | Same flag |
| Short / extended estimate | 2 / 275 minutes | 2 / 275 minutes |

The saved response has no `device.selftest` field, despite its log summary containing the running
entry. The stale entry is in the saved scan data used to render the card. The count of two refers
to the finished history; the synthetic running entry is additional.

**Confirmed:** saved progress is stale relative to the live result, and both inputs to the
refresh-suppression path are present. This explains why a page reload can reproduce `90% left`
from the same saved scan. The source path is strongly supported, but browser requests and deployed
assets have not been independently checked. This remains a likely shared application defect,
not an established ARM-specific failure.

Drive completion and live completion reporting remain verified. The automatic UI completion
check remains **failed**; the subsequent manual recovery result is recorded below.

### Passed — 8.8 manual scan recovery and history display, 2026-11-08

The operator clicked **Scan now** and confirmed:

- The page updated to show the completed short offline test.
- The progress bar disappeared and the short/long test buttons returned.
- The history showed the successful short offline entry at the expected test time.
- Those results persisted across a page refresh.

**Passed for this disk/setup:** manual recovery, successful-history display and persistence of
that display across a browser reload. This supports stale saved scan data as the reason a reload
alone did not help. It does not pass automatic completion or demonstrate persistence across a
container restart, host reboot or image update.

The unread-log notice after recovery was not reported. Its absence is not verified, and there
is no post-scan API capture establishing whether the flag changed. Exact test duration remains
unmeasured. This verification session changed no application code; the defect below was fixed
afterwards.

### Fixed — 8.8 the flag and the skipped live read

The two inputs this section identified were the whole defect, and both are now closed in source.
They are one bug rather than two, and the second half is what made it visible:

- [The parser](../internal/smart/logs.go) raised `self_test_log_unread` for any top-level key
  containing `self_test` that was not one of the two logs it reads. That set includes
  `ata_smart_selective_self_test_log` — the *ranges* a selective test covers, which this app never
  starts and would have nothing to show for, and which most modern ATA drives report. So the flag
  was set on drives whose self-test log was being parsed correctly. It is now raised only when the
  drive offered **no** self-test log this app reads.
- [The card](../internal/webui/assets/js/views/selftest.js) **skipped its live read on mount**
  whenever that flag was set, on the reasoning that the read "could only fail". It could not:
  `smartctl -x` reads every log, and only the parser is limited — and the live read is the only
  thing that corrects a progress bar painted from a stale scan. The card now always reads the drive
  on mount, and a log that genuinely cannot be read is still named in the idle note.
- A third defect in the same area, found while tracing this one: every page re-render built a new
  card, and a new card watching a running test armed a *fresh* poll interval — so a page
  re-rendering more often than the interval never reached a read at all. A `selftest_progress`
  event, a device re-read and the freshness re-read all re-render. The poll deadline is now carried
  across the re-render.

**Retest:** on the same drive, start a short test and leave the page alone. The bar must clear by
itself when the drive finishes, with no `Scan now`. The unread-log notice must **not** appear for
this drive. The Go and JavaScript suites cover the three paths
(`TestParseLogsDoesNotCallASecondaryLogUnread`, `an unread log does not stop the drive being read`,
`a re-render does not postpone the poll`), but neither suite has this drive.

### Failed check — 8.9 SD-card page repeatedly scans, 2026-11-08

The operator could open the SD card's device page. On opening it, the page flickered and repeated
toast notifications said `scan complete: 0/1 readable`. This verifies app visibility and **fails**
the no-repeat-scan check. The subsequent API capture below establishes a device-type detection
error, null successful-scan timestamp and repeated failed device reads. Exact duration/rate and
the detail page's health/SMART-notice wording remain unverified.

**Likely cause from source inspection, supported by the deployed data below:**

1. `startFreshness` in [device.js](../internal/webui/assets/js/views/device.js) calls the scan API
   whenever `last_scan` is absent or invalid. That branch runs even when
   `device_refresh_minutes` is zero.
2. [The store](../internal/store/devices.go) sets `last_scan` only for reads with no error.
   A failed read keeps the previous timestamp, or leaves it null if none succeeded.
3. The scan request can finish successfully at the HTTP/job level while its one device remains
   unreadable. The page's scan callback refreshes the view; independently, the
   `scan_complete` handler in [app.js](../internal/webui/assets/js/app.js) shows the toast and
   redraws the current view. With no successful-scan timestamp, the new view tries again.

This source path matches the symptom and has no CPU-architecture condition. It is a likely
shared application defect, but this SD-card symptom has not been reported or tested on amd64.
The saved record now confirms `last_scan: null`, and the operator confirms the loop stops on
leaving the page, strongly supporting this path. Browser requests and exact deployed assets
have not been independently checked. An old successful timestamp could also cause retries when
the configured refresh interval has elapsed, but this capture specifically has no timestamp.
Disabling that interval does not guard the missing-timestamp branch in source. This verification
session changed no application code; the defect below was fixed afterwards.

### Follow-up — 8.9 loop evidence and dashboard workaround verified, 2026-11-08

The operator confirmed that returning to the main page stops the constant scans; they occur
within the SD-card detail page. The operator then captured:

```sh
docker exec smart-serve-dev wget -qO- \
  'http://127.0.0.1:8080/api/devices'
```

```sh
docker exec smart-serve-dev wget -qO- \
  'http://127.0.0.1:8080/api/scans?limit=5'
```

The SD-card record contains:

| Field | Captured value |
|---|---|
| Serial / device / transport | `0x73a53fad` / `/dev/mmcblk0` / `mmc` |
| Capacity | 31,914,983,424 bytes |
| `last_scan` / `last_seen` | `null` / `2026-11-08T22:09:03Z` |
| `smart_available` / `smart_cause` | `false` / `unknown` |
| `smart_error` and `health_rationale` | `error: /dev/mmcblk0: Unable to detect device type` |
| Health / score / missing | `unknown` / `-1` / `false` |

The five newest scans are IDs **175–179**. Every entry has both timestamps at
`2026-11-08T22:09:03Z`, trigger `device`, `devices_total: 1`, `devices_ok: 0` and
`devices_failed: 1`. These are five distinct failed device reads in the same displayed second;
the API truncates timestamp precision to seconds, so this is not a sustained-rate measurement.
The scan history does not identify the device serial; the SD-card error, matching `last_seen`
and page-specific reproduction connect it to the reported loop.

Each job has `ok: true` and an empty job error: the job completed and recorded an unreadable
device. This does not report a successful SMART read. The device's null `last_scan` persists
because only a successful read sets that field. Returning to the dashboard is a **verified
workaround**, not a fix or a general idle-resource test.

**Other observations from this capture:**

- The saved error is device-type detection, not a captured permission-denial message.
  [The classifier](../internal/smart/failure.go) has no match for this wording and falls through
  to `unknown`. The card has no readable SMART data in this setup; the capture does not prove
  a failed SD card. The detail-page notice still has not been checked.
- The API also ranks the SD card as `fine` with no replacement reasons. This is a replacement
  priority, not a health measurement: [the ranking](../internal/interp/replacement.go) starts in
  that band when no replacement trigger applies, and [the dashboard](../internal/webui/assets/js/views/dashboard.js)
  omits `fine` entries from its replacement shortlist. Retain `health: unknown` as the health
  result; this capture does not demonstrate a false healthy UI label.
- The storage summary names `/etc/resolv.conf`, 19% used, 6,033,821,696 used bytes of
  31,177,043,968 bytes. These are container-visible filesystem figures; host mount/usage parity
  remains unverified.
- Two records are marked present: this SD card and `WD-WCC4M5VVF6RY`, the tested 2 TB WDC disk.
  The WDC record reports `health: good`, `smart_available: true` and a last scan of
  `2026-11-08T22:02:34Z`. Three historical records are marked missing:

| Historical device | Serial | Saved last scan |
|---|---|---|
| Samsung SSD 850 EVO 250GB | `S21NNSAFC00417P` | `2026-11-03T22:28:11Z` |
| WDC WD101PURP-85B5BY0, 10 TB | `VCKJ5Y0P` | `2026-11-05T14:16:21Z` |
| WDC WD101PURP-85B5BY0, 10 TB | `VCKJ95DP` | `2026-11-05T14:18:59Z` |

Those older records do not expand this session's hardware verification or establish a successful
hotplug/reconnect cycle. Reused saved paths such as `/dev/sda` are historical values, not evidence
that all five records are currently attached.

### Fixed — 8.9 the retry loop is bounded

The source path this section describes was exactly right, and it is now closed. The re-read redraws
the page, the redrawn page re-reads the stale drive, and the only thing that ends that is data
arriving — which an unreadable drive never produces. `last_scan` is advanced by a read that
*succeeded* and nothing else, so a drive that cannot be read is stale permanently and the age can
never be the guard.

[The freshness check](../internal/webui/assets/js/views/device.js) now allows **one attempt per
refresh window, per drive**, so five failed reads inside one second become one. A window rather than
a "tried once" flag, so a drive that comes back is picked up on a later visit instead of being
written off for the life of the tab; and the line says the drive could not be read rather than
leaving "reading the drive now…" on screen. `device_refresh_minutes` = 0 keeps its meaning: no
re-read on age at all, and a drive with no data is still read once.

**Retest:** open the SD card's page and watch the scan history. One failed device read per refresh
window is the pass, not the five-in-a-second the capture above records. Reopening the page after
the window elapses should produce one more, and leaving and returning should not. The JavaScript
suite covers it (`a drive that cannot be read is not read over and over`); the hardware does not.

### Next step — 8.9/8.10, verify dashboard health and disconnected-device display

Stay on the dashboard; do not reopen the SD-card detail page for these checks.

1. With display filters clear, confirm that the SD card shows **Unknown** and the tested 2 TB WDC
   disk shows **Good**. Report the actual labels if different.
2. Click **Show 3 disconnected** and check that the Samsung 250 GB SSD and two 10 TB WDC disks
   appear as disconnected records. If they are already shown, use the corresponding **Hide**
   toggle to verify filtering, then restore the preferred view. Do not open their detail pages
   or run scans for this check.

Record the labels/filter behavior before proceeding. This validates rendering of the saved API
state, not physical hotplug detection. The SD-card retry loop and the automatic self-test completion
both have their causes fixed in source and need a retest on this hardware (see the 8.8 and 8.9 fix
notes above); resource/stability, scheduling and restart/reboot checks remain pending.

## Pass 7 — cross-cutting

| # | Do | Expect |
|---|---|---|
| 7.1 | Reload after setting a theme and a unit. | No flash of the wrong theme or the wrong unit |
| 7.2 | Press `s`, `/`, `?`, `Esc`. | Scan; focus the filter; help; close a dialog |
| 7.3 | Narrow the window below 640px. | The sidebar collapses, the toggle works, Scan stays reachable |
| 7.4 | Let a scan finish while you are reading a drive page. | A toast, and **the page does not move** |
| 7.5 | Switch the list to **Table**, tick two drives, and press **Compare 2**. | A dialog with one column per drive, aligned by attribute id |
| 7.6 | Read the sector rows. | The normalised value with the **raw count beneath it**, so a drive with bad sectors is visible |
| 7.7 | Tick a third drive and compare again. | Three columns, in the order they were ticked |
| 7.8 | Try to tick a seventh drive. | The **Compare** button says why it will not go: at most six at once |
| 7.9 | Compare a SATA drive with an NVMe one. | The ATA rows show an em dash for the NVMe column, never a zero |
| 7.10 | Scroll a comparison sideways. | The attribute column stays put |
| 7.11 | Open a drive, go back, and look at the table. | The ticks are still there |
| 7.5 | Watch the network tab while the app is idle. | Nothing. The only repeat request is the self-test card while a test runs |
| 7.6 | Serve the app under a subpath (`/smart-serve/`). | Nothing 404s |

## When something is missing

Check this order before calling it a finding:

1. **Is it conditional?** Almost everything here is. The trigger is named beside it.
2. **Is the data stale?** A drive page is drawn from the last scan. A value that changed since then
   appears after a rescan — that is the design (`FEATURES.md` §E), not a bug.
3. **Did the drive report it?** A blank is often the drive not reporting the value. Those render as an
   em dash on purpose, and **raw output** shows what it sent.

## Reporting a finding

Quote the test number, what you did, and what you saw instead. The procedure is the bug report, so
"3.5, on the SN730: progress appeared but the percentage stayed at 0" is enough to act on.

