# Smart-Serve — manual test plan

`make ci` — `gofmt`, `go vet`, `go test -race` and `node --test internal/webui/jstest/*.test.mjs` —
covers the rules. The race detector is part of it rather than an extra: the scheduler, the SSE hub, the
scan manager and the self-test watcher share one process and one store, and a race between them fails
nothing and corrupts something. This plan covers what they cannot: whether the right thing appears at
all, whether it says the right thing, and whether it is where you would look for it.

**Every test is a numbered procedure.** Do the steps, compare against *Expect*. Anything that does not
match is a finding — quote its number, and the procedure is the bug report.

The passes build on each other, so **run Pass 0 first**. A large part of this UI is *conditional*, and
a card that is correctly absent looks exactly like a card that is broken; Pass 0 makes the conditions
true so the rest of the plan has something to look at.

## Pass 0 — build the test data

| # | Do | Expect |
|---|---|---|
| 0.1 | Press **Scan now** on the fleet. | One row per drive, worst first, and the glance line counts them |
| 0.2 | Name one drive and give it a bay: `curl -X PUT localhost:8080/api/devices/<serial>/notes -d '{"name":"bay 4","bay":"bay 4"}'`. Reload. | The row leads with **bay 4**; that drive's facts list shows **Bay** `bay 4` |
| 0.3 | Tag two drives `tank` and `tank, spare` the same way, sending `tags` beside `name`. | Both groups are offered under **Only drives in these groups** in a channel editor |
| 0.4 | Add a channel (ntfy is the quickest), save it, then press **Test** in its row. | The test message arrives, and the delivery appears under **Recent deliveries** |
| 0.5 | Create a schedule on a five-minute interval. | It runs, and **Last run** fills in |
| 0.6 | Let a second reading land — an hour, or **Scan now**. | **Trends** draws with points |
| 0.7 | Get one drive to caution or failing: lower a threshold in Pass 6, or use `docs/SMART_INTERPRETATION.md` §4. | Its row carries an amber left edge and sorts to the top |
| 0.8 | Point a second channel at a dead host and press **Test**. | A failed delivery, which is what Pass 5's banner needs |

That `PUT` replaces every field, so send `name`, `bay` and `tags` together or the ones left out are
cleared.

Three conditions Pass 0 cannot create, and the passes that need them say so: a **disconnected** drive
(unplug one), an **NVMe** drive, and a drive with a **partition table** if the fleet has none.

## Pass 1 — the fleet

| # | Do | Expect |
|---|---|---|
| 1.1 | Open `/`. | Title **Fleet**, the glance line under it, **Scan now**, a table of Drive, Health, Usage, Temp and Powered on, then `N drives` with **Schedules** and **Settings** links |
| 1.2 | Read the glance line. | Four counts in the order failing, caution, healthy, no reading, then `Checked X ago` — or `Never scanned` |
| 1.3 | Click a count. | The table narrows to those drives, that count is pressed and the others are not, and the totals stay as they were |
| 1.4 | Click the same count again. | The filter clears and every drive is back |
| 1.5 | Filter, open a drive, come back with **← Fleet**. | The filter is still applied |
| 1.6 | Hard-refresh with a filter applied. | It is cleared — the filter is per visit, not stored |
| 1.7 | Click a count that shows 0. | `No drives match.` |
| 1.8 | Read a row. | Name, then model · transport · capacity · mountpoint; Health as dot, word and score; Usage as a bar and a percentage; Temp; Powered on in hours |
| 1.9 | Read the order with a mix of states. | Failing, caution, no reading, healthy — and within a state the lower score first, the name breaking a tie |
| 1.10 | Look at the left edge of a caution or failing row, then of a healthy one. | An amber edge on the first cell; nothing on the healthy row |
| 1.11 | Unplug a drive and scan. | Its row stays, Health reads **Not connected**, and it counts under no reading |
| 1.12 | Hover a row, then click it. | The name underlines and the drive page opens |
| 1.13 | Tab to a row and press Enter; press Space on another. | Both open the drive page |
| 1.14 | Read the Usage cell of a drive whose filesystem the container can see, then open that drive. | The bar and percentage on the row match the **Disk space** figure on its page |
| 1.16 | Type part of a drive's name, then its bay, then a serial, then a tag. | The table narrows to the matches each time; the counts above it stay as they were |
| 1.17 | Press `/`. | The search box takes focus; typing filters, and the box keeps the caret. `Esc` then a click clears it |
| 1.15 | Press **Scan now**. | It reads `Scanning…` and is disabled; a toast `Scan complete: N/M readable` when it ends, and the table redraws in place |

## Pass 2 — one drive's page

| # | Do | Expect |
|---|---|---|
| 2.1 | Open a healthy drive. | **← Fleet**, the name, model · transport · firmware, serial, and the verdict: dot, word, `score N of 100`, and the reason when there is one |
| 2.2 | Press **Scan this drive**. | It reads `Reading…`, then the page redraws with a fresh `data from …` line, and the scroll position does not change |
| 2.3 | Press **Export…**. | An HTML report for that drive downloads |
| 2.4 | Read the line under the name on a recently read drive. | `data from 2 minutes ago` |
| 2.5 | Open a drive last read four hours ago, with **Re-read a drive on open** at its default 15. | `4 hours ago, reading the drive now…`, then the page redraws with fresh data |
| 2.6 | Set it to 0 and reopen that drive. | The age is still shown; nothing is read |
| 2.7 | Open a drive that has never been scanned. | It is read whatever the window says: `no data yet, reading the drive now…` |
| 2.8 | Open a drive whose read always fails (an SD card, or a reader with no card). | `…, could not re-read the drive`; one failed read per window, not five in a second; the page keeps the data it had |
| 2.9 | Read the facts list. | Capacity, Powered on, Wear, Temperature, Mounted at, Device node, Bay, Last scanned — each a figure, `n/a` or `never` |
| 2.10 | Read **Trends**. | Six plots in two rows of three — Health score, Temperature, Power-on hours, Wear, Storage, Bad sectors — each with its latest figure and its change |
| 2.11 | Press 7d, then 30d, 90d and All. | The plots redraw, the pressed button follows, the reading count beside them changes, and the page does not move |
| 2.12 | Open a drive with one reading. | `not enough readings` where a plot would be |
| 2.13 | Hover a plot. | A readout follows the pointer, sits on a reading, and one dot per reading is drawn |
| 2.14 | Focus a plot and press ←/→. | The readout moves one reading at a time |
| 2.15 | Look under the plots on a window spanning more than a day. | A date axis; a window inside one day has none |
| 2.16 | Scan one drive a day apart. | Every plot gains a point, and **Storage** reports its change in points |
| 2.17 | Read the **Attributes** heading. | `N reported · none at threshold` — or `N at threshold` |
| 2.18 | Read a row. | ID, Attribute, Value, Worst, Thresh and Raw |
| 2.19 | Press a **?**. | A dialog saying what the attribute is and what to watch, in plain words |
| 2.20 | Find an attribute at or below the drive's own threshold. | The row is marked and carries **At threshold** |
| 2.21 | Find a row smartctl has no name for. | **Vendor specific** in muted text, with a note under the table saying why — never `Unknown_Attribute` |
| 2.22 | Open a drive that reports no S.M.A.R.T. | `This drive does not report S.M.A.R.T. data, so there are no attributes to show.`, with the drive's own error under it |
| 2.23 | Open an NVMe drive. | The section is **NVMe health log**, in groups, with **?** on its fields — not an ATA table |
| 2.24 | Read the **Disk space** usage line. | `NN% used` and `of 1.8 TB on /mnt/tank` |
| 2.25 | Open a drive with a partition table. | A map above the table, one segment per top-level partition, sized by its share of the disk; the usage line carries no bar of its own |
| 2.26 | Read the table's columns. | Partition with its label, its node beneath and a **Read-only** badge where the drive says so; Size; Filesystem; Mounted at; Used; and a **UUID** button |
| 2.27 | Point at a map segment. | Label, size, usage and mountpoint appear at once — no browser tooltip delay — and the matching row is marked |
| 2.28 | Point at a table row. | The matching segment is marked; a row with no segment marks nothing |
| 2.29 | Look at a disk with free space. | A hatched free segment whose tooltip says the figure is approximate |
| 2.30 | Look at a drive whose partitions leave only an alignment remainder. | No free segment; the map's background shows through |
| 2.31 | Look at a drive with a volume group on it. | The logical volume is a table row only, never a segment |
| 2.32 | Look at a partition the container cannot see. | Its segment is hatched, not filled at 0%, and Used reads `not visible` |
| 2.33 | Look at an unmounted partition. | Used reads `n/a` |
| 2.34 | Press **UUID** on a row. | `UUID copied`, and it pastes as what `blkid` reports |
| 2.35 | Press **UUID** on a partition holding a volume group. | It copies the partition's UUID — that partition has no filesystem of its own |
| 2.36 | Set Thresholds → **Storage warning level** to 50, save, reopen the drive. | Used bars at or above 50% are amber |
| 2.37 | Check a 95%-full drive's verdict. | Unchanged — the level colours a bar, it does not move the verdict |
| 2.38 | Turn **Storage usage** off in Appearance. | The Used column is gone — not blank — and the usage line and the map stay |
| 2.39 | Add `- /mnt:/mnt:ro` to the compose file and restart. | Used fills in for filesystems under `/mnt` |
| 2.40 | Open a disk with no partition table. | One note saying the drive has no table, holds no filesystems, or nothing readable from inside the container — not an empty table |
| 2.41 | Scroll to the bottom. | `Read from /dev/sda 2 hours ago`, or `Never scanned` |

Every figure above comes from the last scan unless the page says it is reading the drive — that is the
design (`FEATURES.md` §E), not a bug.

## Pass 3 — self-tests

The only part of the app that writes to a drive, and the only part that reads one on a timer. Do this
pass on a spare drive if you have one.

| # | Do | Expect |
|---|---|---|
| 3.1 | Read the **Self-test** section on an idle drive. | **Short test** and **Long test**, each with the drive's estimate beside it (`· 2 min`) when the drive reports one, and a line saying what a test costs |
| 3.2 | Press **Short test** and confirm. | A toast, then a progress bar, `NN% left`, and **Stop test** |
| 3.3 | Leave the page alone until it finishes. | The bar clears by itself, the buttons return, and the log below gains the completed result — with no **Scan now** |
| 3.4 | Reload. | The result is still in the self-test log, which is inside this section, not elsewhere |
| 3.5 | Start one, then press **Stop test** mid-run and confirm. | A toast, and the section returns to idle |
| 3.6 | Stop a test behind a bridge that ignores the abort. | `The drive accepted the abort but is still reporting a test in progress…` — never a claim that it stopped |
| 3.7 | Look at the section while a test runs. | **Stop test** only: no run button to press twice, and nothing for the drive to refuse |
| 3.8 | Press **Long test** on a drive that refuses one. | The note says the drive refused it and **Long test** is greyed out — never a bare `smartctl exit status 4` |
| 3.9 | After 3.8, look at **Short test**. | Still usable — a refused long test says nothing about the short one |
| 3.10 | Open a drive that cannot self-test. | It says so, with no buttons offered |
| 3.11 | Open a drive whose self-test log cannot be read. | The note names the unread log, and the buttons stay: a log that failed to read is not a drive that failed to test |
| 3.12 | Start a test, go to the fleet, come back. | Progress is read from the drive, not counted from when the page was opened |
| 3.13 | Watch the network tab while a test runs, then navigate away. | A read every 15 seconds while the section is on screen; none once it is not |
| 3.14 | Set Appearance → **Self-test refresh** to 5. | The note quotes 5 seconds and the requests follow it |
| 3.15 | On a drive that reports no percentage while testing. | The bar is the drive's own estimate, labelled as an estimate, and it stops short of full |

## Pass 4 — schedules and self-test passes

| # | Do | Expect |
|---|---|---|
| 4.1 | Press **New schedule**. | Label, Action, Repeat, Enabled, Cancel and Save |
| 4.2 | Set Repeat to every 5 minutes and save. | The preview reads `Runs every 5 minutes.`; the row shows **Next run** `in 5m` with the date beneath it |
| 4.3 | Press **Run now**. | A toast `Schedule started` |
| 4.4 | Switch Repeat to **On chosen days, at a time**. | An **At** time and a seven-cell day strip appear |
| 4.5 | Choose **Cron expression** and type `0 3 * * *`. | The preview reads `Runs every day at 03:00.` |
| 4.6 | Type `0 3 * *` and press **Save**. | The preview reports an error, and Save refuses with a toast naming the five fields; the dialog stays open |
| 4.7 | Type `x x x x x`. | The preview says it could not interpret that expression — inline, before any round trip |
| 4.8 | Set **Action** to **Send a digest of the period**. | A hint saying the digest goes through the notification channels, and that a channel with no events ticked receives it |
| 4.9 | Set it back to scan. | The hint goes |
| 4.10 | Set **Action** to **Run a self-test on each drive**. | A **Test** picker (Short, Long) and a hint that it runs one drive at a time and is the one schedule that writes to the drives |
| 4.11 | Save it with Test short. | The row's sub line reads **Self-test · short** |
| 4.12 | Press **Run now** on it. | The toast says `Self-test pass queued`, not that a scan started |
| 4.13 | Look at the top of the page. | A banner: `Self-test pass running, 0 of N drives done`, `testing <drive> now`, and a progress bar |
| 4.14 | Watch it work. | One drive testing at a time; the next starts only after the previous finishes |
| 4.15 | Press **Run now** again during a pass. | The fire is skipped and the app log says a pass is running — no second pass |
| 4.16 | Restart the app mid-pass. | The pass stops, the drive that was testing keeps testing, and the next fire skips a drive already testing itself |
| 4.17 | Turn a row's **On** switch off. | **Next run** reads `paused`, the row dims, and nothing runs |
| 4.18 | Turn it back on. | A next run returns |
| 4.19 | Press **Delete** on a schedule. | A confirmation naming it, then it goes |
| 4.20 | Let a digest fire. | It reaches the channel, and **Recent deliveries** records it |

## Pass 5 — notifications

| # | Do | Expect |
|---|---|---|
| 5.1 | Open `/notifications`. | Title **Notifications**, **New channel**, and a glance line |
| 5.2 | Read the glance line. | N channels, N enabled, N event types routed, then `last delivery X ago` — or `no deliveries yet` |
| 5.3 | Press **New channel** and open **Type**. | ntfy, Gotify, Telegram, Discord, Email (SMTP), Webhook (generic HTTP POST) |
| 5.4 | Fill it in, then press **Test** three times. | Three test messages, `Test event sent` each time, the button usable each time, and no new row in the feed — a draft test is not a delivery |
| 5.5 | Click beside the dialog. | It stays open; Escape closes it |
| 5.6 | Save and read the row. | The channel's label with its type beneath, and **Test**, **Edit**, **Delete** |
| 5.7 | Tick some events and save. | **Told about** carries one chip per event; with none ticked it reads **Every event** |
| 5.8 | Read the **Drives** cell of a channel with no group ticked. | **Every group** |
| 5.9 | Tick a group (Pass 0 tagged one) on that channel and save. | One chip per group in **Drives**, and the cell no longer reads **Every group** |
| 5.10 | Reopen the channel without changing anything and save again. | The group is still ticked — the scope must not be quietly widened |
| 5.11 | Press **Test** on the scoped channel. | It still delivers: a scope must not make a channel untestable |
| 5.12 | Press **Test** on the channel pointed at a dead host (Pass 0). | `Test failed: …`, and a red banner above the channel table: `1 recent delivery failed`, naming the channel and the detail |
| 5.13 | Fix that channel and Test again. | The new delivery shows as sent in the feed; the banner still counts the older failure, which is still in the log |
| 5.14 | Read **Recent deliveries**. | One row per delivery — when, channel, event, status, detail — and `N recorded · the last 25 are kept here` |
| 5.15 | Press **Test** and watch the feed. | It refreshes in place, and the page does not jump to the top |
| 5.16 | Delete every channel and look at the table's place. | `No channels yet, add one and a failing drive will tell you about it.` |
| 5.17 | **Add window**: one-off, now to +10 minutes. | Listed with its time range and the state **active** |
| 5.18 | Add a daily and a weekly window. | Both **recurring**, with the schedule in words (`Every day, 02:00–04:00`) |
| 5.19 | Add a window starting tomorrow. | **upcoming** |
| 5.20 | Scope a window to two drives, then look at it. | `Drives: <names>`; with none ticked, `All drives` |
| 5.21 | Delete every window. | `No maintenance windows, every drive alerts all the time.` |

## Pass 6 — settings

| # | Do | Expect |
|---|---|---|
| 6.1 | Open `/settings`. | The **Thresholds** page: its name, one sentence saying what it is for, then rows of label, help and control |
| 6.2 | Look at the bottom of the window with nothing changed. | No save bar |
| 6.3 | Change one threshold. | A bar appears at the bottom: `Unsaved changes`, **Revert**, and an enabled **Save** |
| 6.4 | Press **Save**. | A toast `Settings saved`; the bar goes and Save is disabled again |
| 6.5 | Read a row whose help runs to two sentences. | The first is shown, and **Why?** opens the rest |
| 6.6 | Change a threshold and navigate away. | `Leave without saving?` — Cancel stays, Discard leaves |
| 6.7 | Change one, Save, then navigate away. | No prompt |
| 6.8 | Open `/settings/thresholds` in a fresh tab, then `/settings/nonsense`. | The first is that page; the second lands on Thresholds, and so does `/settings` |
| 6.9 | Drag a slider, then type into its number box. | The two agree, and the unit beside them follows the temperature unit |
| 6.10 | Switch **Temperature unit** to Fahrenheit, then open Thresholds and Notifications. | The temperature threshold and the correlated-heat rise are shown in °F; the rise converts as a span, so 8 °C reads 14 °F, not 46 |
| 6.11 | Read the last slider on Thresholds. | **Storage warning level**, 90% by default — the figure 2.36 changes |
| 6.12 | Press **Reset to recommended**. | The controls move to the shipped values, the bar says unsaved, and nothing is written until Save |
| 6.13 | Press **Scan now and re-check**. | A toast `Scan started, verdicts update as each drive is read` |
| 6.14 | Press **Load the fleet's models**. | One entry per model at the values above, and `N models in the fleet` |
| 6.15 | Change one number in one entry and Save, then reload. | The change is still there |
| 6.16 | Press **Load the fleet's models** again. | The entry just edited is kept, not overwritten |
| 6.17 | Paste `{"870 EVO": {"reallocated_bad": "1"}}` into the per-model editor and Save. | An error beside the box naming the unknown threshold and the keys it accepts; nothing saved |
| 6.18 | Paste `{"nope": 1}` and Save. | An error beside the box saying it must be a JSON object of model name → thresholds; nothing saved |
| 6.19 | Click a theme swatch. | The app recolours at once and the swatch is marked; Save, reload, and it is still that theme |
| 6.20 | Pick **Custom**. | **Custom base** and **Accent colour** appear; a named palette hides them |
| 6.20a | On `/settings/appearance`, turn **Partitions on the drive page** off and save, then open a drive that has a partition table. | The usage figure stays; the disk map and the partition table are gone |
| 6.20b | Turn it back on and save. | Both return on the next visit to the drive |
| 6.21 | Open `/settings/notifications`. | Squelch window, history retention, notification log retention, the two correlated-heat numbers, and **Public URL** |
| 6.22 | Open `/settings/data`. | Import and export, **Serve /metrics**, and the knowledge base |
| 6.23 | Press **Export**, then **Import** that file. | A JSON bundle; Import confirms first, says a file carrying schedules replaces the list, then names how many labels, settings and schedules were applied |
| 6.24 | Read the knowledge base status line. | `N families known · N shipped with this build · nothing fetched yet` |
| 6.25 | Press **Refresh now**. | A toast `Fetched N families`, the status gains an `N fetched … ago` clause, and the page does not jump |
| 6.26 | Press **Download the shipped table**. | A JSON file |
| 6.27 | Paste `not json` into **Your own entries** and Save. | The error is shown beside the box, naming the field |
| 6.28 | Press **Discard the fetched copy** when one exists. | The status line drops back to the shipped count, and the button goes |
| 6.29 | Turn on **Serve /metrics**, save, then fetch `/metrics`. | The endpoint answers with its metrics; with the switch off it is a 404 |

## Pass 8 — arm64 (Raspberry Pi)

**Status: both defects found here are fixed and retested; idle acceptance and stability remain open.
Updated 2026-11-11.** Smart-Serve runs on a Raspberry Pi 5 with DietPi, and the SSH outputs and operator
reports confirm native arm64 execution, HTTP health, device-access configuration, USB SAT discovery, a
full container smartctl read of the attached WDC disk, and the app's displayed identity, attribute count
and raw values. The 2026-11-08 session then turned up two defects — a short test whose progress never
cleared, and an SD-card page that re-read itself in a loop — both diagnosed from that session's own
captures and fixed (8.8, 8.9). **Both were retested on 2026-11-11 against `v1.1.0-28-g6fee3dc` and
passed**, on this Pi *and* on the amd64 host, together with the dashboard and disconnected-device checks.
The idle budget is the one ARM criterion still unsettled: CPU is confirmed near zero, but this host
reports no usable Docker memory figure, so the under-20-MB claim cannot be measured here with the same
metric the reference host used (8.5).
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
| 8.5 | Ten-minute idle CPU/RSS measurement with no browser, scan or self-test active. | **Passed on both halves, 2026-11-18.** Eleven samples over ten minutes on `v1.1.0-33-ga7e6449`, nothing else running: **RSS 20,000-20,032 kB (19.5 MiB)** and **PSS 19,984-20,016 kB (19.5 MiB)**, both flat to within 48 kB; **11 file descriptors and 11 threads**, constant; **14 CPU ticks in 600 s**, 0.14 CPU-seconds, **0.023%** of one core. The two memory figures agreeing to within 48 kB is what a single-process container looks like: almost nothing is shared, so RSS is not inflated by another process's pages. The cgroup figure still cannot be read on this host (no memory controller, `docker stats` reports `0B / 0B`), so the criterion is met on **PSS**, which is the better number anyway because it does not double-count shared pages. The amd64 host reads **18.9 MiB** by the cgroup and agrees. |
| 8.6 | NVMe temperature, health fields and comparison with amd64. | **Not tested.** No NVMe device appears in this capture. |
| 8.7 | Overnight scheduled scans and stable memory/file-descriptor usage. | **Not tested.** Startup and later log timestamps alone do not establish a controlled stability run. |
| 8.8 | App-triggered short self-test: start, progress, completion and result after page reload. | **Drive completion passed; UI running-state check failed, and the cause is now fixed — retest needed.** Direct smartctl output shows execution status 0 and the newest short test `Completed without error` at 45,446 hours. The operator confirmed the app continues showing `90% left` and only **Stop test** after extended waiting and manual reload. The live API confirmed completion (`passed: true`, no `running`) but also set `self_test_log_unread` for the selective-test log; the saved record from 16:02:49 UTC (scan 18) carried that flag and `remaining: 90`. **Fixed:** the flag was raised for a log the app reads (see the 8.8 fix note below), and the card no longer skips its live read on mount. **Manual recovery and completed-log visibility passed:** after **Scan now**, the progress bar disappeared, short/long buttons returned and the successful short offline entry was visible; these results survived a page reload. **Retested 2026-11-11 on `v1.1.0-28-g6fee3dc` — passed**, on this Pi and on the amd64 host, in both variants: staying on the page, the progress cleared **on its own** when the drive finished, the buttons returned and the log gained the completed entry; leaving the page and watching the fleet, the testing badge tracked the run and cleared with it. No unread-log notice appeared. Exact duration remains unverified. |
| 8.9 | SD-card discovery in the app, unsupported-SMART notice and absence of repeated automatic scans. | **App visibility verified; no-repeat-scan check failed, and the cause is now fixed — retest needed.** The API reports `last_scan: null`, `smart_available: false`, health/cause `unknown`, and `Unable to detect device type`. Scan IDs 175–179 each record one failed device read at 22:09:03 UTC. The operator confirms the repeated scans occur on the SD-card page and stop on returning to the dashboard. **Fixed:** the staleness re-read is now bounded to one attempt per refresh window, per drive. **Retested 2026-11-11 on `v1.1.0-28-g6fee3dc` — passed:** leaving the SD card's page open produced no flicker and no toast storm. The per-window read count was not re-measured in this retest, and the detail-page notice remains unverified. |
| 8.10 | Disconnected-device inventory and dashboard filter. | **Passed 2026-11-11.** With filters clear the SD card reads **Unknown** and the tested 2 TB WDC disk reads **Good**, and **Show 3 disconnected** lists the three historical records. This verifies rendering of the saved API state, not physical hotplug detection. |

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
- Docker reported memory as `0B / 0B` on the Pi, which is not a usable memory measurement; its cause
  remains unverified. The process's own `VmRSS` from `/proc/1/status` is the only figure available, and
  it is not the metric the MVP criterion names — see 8.5. Keep container memory and process RSS separate
  when reading any figure from this host.
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

### Defect found — 8.8 self-test progress did not clear

**Symptom:** a short test started, the page updated every 15 seconds, and it stayed at
`In progress, 90% left` after waiting and reloading. A direct read of the drive showed the test had
**completed without error** at 45,446 lifetime hours, above two earlier `Aborted by host` entries (50%
and 10% remaining) — the 10% one being a test the operator had stopped by hand.

**Cause, confirmed from the operator's saved scan record** — two inputs, one bug:

1. `self_test_log_unread` was raised for any unrecognised key containing `self_test`, and that set
   includes `ata_smart_selective_self_test_log` — the ranges a selective test covers, which most
   modern ATA drives report. So the notice appeared on drives whose self-test log was being read
   correctly.
2. The card **skipped its live read** whenever that flag was set, on the reasoning that the read
   "could only fail". The live read is the only thing that corrects a progress bar painted from a
   stale scan, so the saved record's stale `remaining: 90` stayed on screen across reloads.

**Fixed** (`FEATURES.md` A20): the flag is raised only when the drive offered **no** self-test log this
app reads, and the card always reads the drive on mount. A third defect in the same area is fixed too:
every re-render armed a fresh poll interval, so a page re-rendering more often than the interval never
reached a read at all. The deadline is now carried across the re-render.

**Retest:** on the same drive, start a short test and leave the page alone. The bar must clear by
itself when the drive finishes, with no **Scan now**, and the unread-log notice must **not** appear for
this drive. Covered by `TestParseLogsDoesNotCallASecondaryLogUnread`,
`an unread log does not stop the drive being read` and `a re-render does not postpone the poll` — but
no suite has this drive.

The same symptom is reported on amd64, so an ARM-specific fault is not established. Manual recovery
passed during the session: **Scan now** cleared the bar, restored the short/long buttons, and showed
the successful entry, persisting across a reload.

### Defect found — 8.9 the SD card's page scanned in a loop

**Symptom:** opening the SD card's page flickered and rolled in `scan complete: 0/1 readable` toasts.
The API capture shows why: `last_scan: null`, `smart_available: false`,
`error: /dev/mmcblk0: Unable to detect device type`, and five scan records (IDs **175–179**) in the
same displayed second, each `devices_ok: 0` / `devices_failed: 1`. The SD card's own record:

| Field | Captured value |
|---|---|
| Serial / device / transport | `0x73a53fad` / `/dev/mmcblk0` / `mmc` |
| Capacity | 31,914,983,424 bytes |
| `last_scan` / `last_seen` | `null` / `2026-11-08T22:09:03Z` |
| `smart_available` / `smart_cause` | `false` / `unknown` |
| Health / score / missing | `unknown` / `-1` / `false` |

**Cause:** `startFreshness` re-reads whenever `last_scan` is absent, and `last_scan` is advanced only
by a read that *succeeded* — so an unreadable drive is stale permanently and the age can never be the
guard. The re-read redraws the page, the redrawn page re-reads, and only data arriving ends that.
Returning to the dashboard stops it, which was the workaround.

**Fixed** (`FEATURES.md` A28 follow-up): one attempt per refresh window, per drive.
`device_refresh_minutes` = 0 keeps its meaning, and a drive with no data is still read once.

**Retest:** open the SD card's page and watch the scan history. One failed device read per refresh
window is the pass, not five in a second, and reopening after the window elapses should produce one
more. Covered by `a drive that cannot be read is not read over and over`; the hardware does not.

### Retested — 8.8 and 8.9 both pass, 2026-11-11

Both defects were retested against `v1.1.0-28-g6fee3dc` — the local development image built from the
fixes — on this Pi **and** on the amd64 host. Same actions that failed on 2026-11-08:

| Check | Result |
|---|---|
| 8.8 short test, staying on the page | **Passed, both hosts.** Progress cleared on its own when the drive finished; the short/long buttons returned; the log gained the completed entry. No **Scan now** needed, and no unread-log notice. |
| 8.8 short test, leaving for the fleet | **Passed, both hosts.** The testing badge tracked the running test and cleared when it finished. |
| 8.9 SD-card page left open | **Passed.** No flicker, no `0/1 readable` toast storm. |
| 8.10 dashboard health and disconnected display | **Passed.** SD card **Unknown**, WDC disk **Good**; **Show 3 disconnected** lists the three historical records. |

The retest was against a **local build**, not the published image — the source is identical and CI
builds the same Dockerfile, but the record should say so. Re-running the 8.8 check against the
published tag takes about four minutes and is worth doing if the release needs that stronger claim.

### Still open on this hardware

1. **Idle budget (8.5) — closed on PSS, 2026-11-18.** The cgroup still cannot account memory on this
   host (no memory controller; `docker stats` reads `0B / 0B`), and enabling it would mean adding
   `cgroup_enable=memory cgroup_memory=1` to `/boot/firmware/cmdline.txt` and rebooting. Neither is
   needed: RSS and PSS were sampled for ten minutes and agree at 19.5 MiB, inside the 20 MB budget,
   with fds, threads and CPU flat. The criterion is therefore stated against PSS.
2. **8.7 overnight stability** — scheduled scans and stable memory/file-descriptor use over a full night.
3. **8.6 NVMe** — no NVMe device is attached to this host.
4. **Restart/reboot persistence**, and identity across a different enclosure bay.

## Pass 7 — cross-cutting

| # | Do | Expect |
|---|---|---|
| 7.1 | Reload after setting a theme and a unit. | No flash of the wrong theme or the wrong unit |
| 7.2 | Press `s`. | A scan starts (`Scan started`); with the cursor in a text field it types `s` instead |
| 7.3 | Open a dialog and press Escape. | It closes |
| 7.4 | Narrow the window below 640px. | The sidebar collapses behind the toggle, the toggle opens it, and **Scan now** stays reachable |
| 7.5 | Let a scan finish while reading a drive page. | A toast, and the page does not move |
| 7.6 | Serve the app under a subpath (`/smart-serve/`). | Nothing 404s — navigation, the report link and the export all keep the prefix |
| 7.7 | Watch the network tab while the app is idle. | Nothing. The only repeat request is the self-test read while a test runs |
| 7.8 | Open an unknown path (`/nonsense`). | The fleet, not a blank page |
| 7.9 | Scroll to the bottom of a drive page and click the wordmark. | The fleet opens at the top |

## Pass 9 — sign-in (optional layer, `ADR-033`)

Off unless `SMARTSERVE_AUTH_USER` and a password are set; with neither, nothing in this pass applies
and the app has no login at all. Restart the container between steps that change the environment.

| # | Do | Expect |
|---|---|---|
| 9.1 | With neither variable set, open the app and the sidebar. | No login page, no **Sign out**, and `GET /api/session` answers `{"enabled":false}` |
| 9.2 | Set only `SMARTSERVE_AUTH_USER` and start the container. | It **refuses to start** and names the missing password variable |
| 9.3 | Set both, then open `/`. | The sign-in page, not the app: username, password, **Sign in** |
| 9.4 | Enter the wrong password. | The same sign-in page with one vague message; nothing says which half was wrong |
| 9.5 | Sign in correctly. | The page you asked for opens (`next` is honoured), and the sidebar gains **Sign out** |
| 9.6 | Reload, and open a second tab. | Still signed in, so the session is a cookie and not per-page state |
| 9.7 | Click **Sign out**, then press Back. | The sign-in page: the token is revoked server-side, so a copied cookie is dead too |
| 9.8 | `curl -u user:pass http://host:port/api/devices`. | `200` with the JSON — Basic works without any session |
| 9.9 | `curl http://host:port/api/devices` (no credential). | `401` with `WWW-Authenticate: Basic` and `{"error":"authentication required"}` |
| 9.10 | `curl http://host:port/healthz` and scrape `/metrics` with Basic. | Both answer without a session: a probe cannot sign in, and the scraper sends the credential |
| 9.11 | Set **Settings → Data → Sign-in → Idle timeout** to 1, save, sign in and wait two minutes. | The next page load returns to the sign-in page |
| 9.12 | Set `SMARTSERVE_AUTH_PASSWORD_FILE` to a file holding the password, remove the inline variable, restart. | Sign-in works with the file's contents (one trailing newline is not part of the password) |

## When something is missing

Check this order before calling it a finding:

1. **Is it conditional?** Almost everything here is. The trigger is named beside it.
2. **Is the data stale?** A drive page is drawn from the last scan. A value that changed since then
   appears after a read — that is the design (`FEATURES.md` §E), not a bug.
3. **Did the drive report it?** A blank is often the drive not reporting the value. Those render as
   `n/a` on purpose.

## Not covered here

- **Error-log entry lists** — no drive here with a readable error log to test against, and the page
  has no error-log section yet.
- **Which attribute the verdict blamed** — no drive here produces a penalty that names one, so the
  marking is untested.
- **RAID, LVM and ZFS member naming** — no hardware to test against. A drive with no readable table
  gets one generic note.

## Reporting a finding

Quote the test number, what you did, and what you saw instead. The procedure is the bug report, so
"3.3, on the SN730: the bar cleared but the self-test log gained no row" is enough to act on.
