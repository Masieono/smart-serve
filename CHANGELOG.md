# Changelog

Notable changes per release. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

The drive's own data, the fleet tier, the knowledge base, and the notification polish. Self-tests
can be started, watched and stopped from the device page, and the drive's error and self-test logs
are rendered. The dashboard gained filtering, sorting, a dense table, tag grouping and a "replace
first" ranking. Each drive and the whole fleet export as a warranty evidence pack. Alerts gained
acknowledgement, action buttons back into the app, and a weekly digest.

### Added

- **Reset to recommended**, beside the health thresholds: it loads the shipped values into the controls
  and does not save them. That distinction is the point — somebody who does not know what a reasonable
  threshold *is* can see one, compare it with what they had, and change their mind, which a reset that
  wrote straight to the database would take away. Save is right there, and the note above it says the
  values take effect from the next scan.
- **Drive knowledge base (`A21`)** — rated endurance and known firmware advisories, in **three layers**
  that win in order: a table bundled with the build, a copy fetched from a configurable URL, and your
  own entries. The bundled file and the refreshable file are **the same file**, so there is one format,
  one validator, and one thing for a contributor to learn — which is what lets the data outlive the
  repository. The refresh is a **button in Settings, never a schedule**, because a knowledge base that
  quietly polled a URL would be the first thing to break the zero-idle-cost invariant, for data that
  changes a few times a year; the card shows the copy's age so pressing it is an informed decision.
  Endurance is stored **per capacity**, because a 250 GB and a 4 TB drive in one family are rated
  differently, and a capacity no bucket is close to gets *no* figure rather than a neighbour's. Every
  figure carries its **source**, shown beside the number: a figure nobody can check is a figure nobody
  should believe. Advisories appear only when the firmware matches — a drive that reported no firmware
  cannot match a firmware-specific one — and the knowledge base **moves no verdict**, because a
  hand-edited table that could move a verdict would make the verdict unaccountable. The format is
  documented in `docs/KNOWLEDGE.md`.
- **Weekly digest (`B13`)** — one summary of a period instead of a message per event: the fleet as it
  stands, what changed, what lost storage, how many scans ran, and what the ranking says to replace
  first. It is a **schedule with `action: digest`** rather than a setting with its own timer, because a
  weekly summary is the same timing problem as a scan on a schedule — so it inherits cron, time zones
  and the missed-run policy. The period is *since this last ran*, so a monthly digest does not repeat
  itself, and a drive that was not being watched at the start is not reported as a change. Its
  priority reflects its content: a healthy fleet is a note, a failing drive is not. Sections with
  nothing in them are omitted, and each list is capped and says what it left out.
- **Prometheus `/metrics` (`C6`)** — an opt-in exporter in the text exposition format: per-drive score,
  temperature, power-on hours, wear, new sector problems and last verdict as labels, plus fleet counts
  and the last scan time. **Off by default** (`prometheus_enabled`), and a disabled endpoint answers
  `404` rather than `403`. A reading the drive did not report **drops the series rather than exporting
  a zero** — an absent series means "no measurement", which is what Prometheus expects and what the em
  dash means on screen. Written by hand rather than with the client library: four rules and six metrics
  do not justify multiplying a dependency tree that holds one module.
- **Alert acknowledgement (`B7`)** — an **Acknowledge** control on a drive's page silences the alerts
  for a condition the user already knows about, with a note saying why ("RMA raised"), and leaves the
  verdict alone: the drive still reads as caution, because it is caution. What it removes is the
  repetition, not the finding. The ack records **the verdict it was granted for**, so it is **spent
  the moment the verdict moves** — a drive acknowledged at caution that goes bad alerts normally. That
  is a better expiry than a timer: it is exactly the condition the user named, and it happens when
  there is something new to say. A drive that vanishes and a self-test the user asked for still alert,
  because an acknowledgement covers a condition rather than a drive. The device page says outright
  that the condition is acknowledged, with the note and when — without that the reader would conclude
  the app had lost the alert, or that the drive had recovered.
- **ntfy action buttons (`B10`)** — a notification now carries a **view action back into the app**:
  "Open drive" for a device event, "Open dashboard" otherwise, with `clear=true` so the notification
  is dismissed once opened. The URL comes from a new **`public_url`** setting, deliberately empty by
  default: the app cannot know its own external address, and guessing `localhost` would produce a
  button that works for nobody while looking like a feature. Priority and tags were already there.
- **Correlated change (`A25`)** — the fleet-only insight: eight drives in one enclosure share a
  temperature, so a dying fan presents as *all eight* running hotter at once, which per-drive
  monitoring reports as eight unrelated alerts. The app now notices that a group moved together and
  says so **once**, naming the group and the shared rise. It reports a **correlation and never a
  cause** — nothing observable from a temperature separates a stopped fan from a RAID rebuild or a
  warm afternoon — so the wording names what moved and where to look and leaves the diagnosis to the
  reader. Temperature only, because that is what an enclosure shares; a *rise* rather than a level;
  two configurable thresholds (how many drives must move, and by how much); and the notification is
  **opt-in per channel**, because a false "your enclosure is overheating" would be worse than the
  eight true alerts it explains. The per-drive alerts are deliberately *not* suppressed: each is
  true, and what `A25` removes is the confusion rather than the messages.
- **Testing session (`D11`)** — the `bench` tag *is* the session: no mode, no new column, no new
  control. A drive tagged `bench` is being evaluated rather than used, so it is left out of the
  health counts and out of the replace-first ranking while staying listed, searchable and in every
  export. Without that, a batch of drives you already expect to fail would dominate the ranking above
  the drive in service that is actually failing. The exclusion is **stated on screen**, not silent. A
  card appears while the bench is not empty and carries the batch's verbs — **Test all**, **Export
  batch** (the fleet CSV scoped to the tag) and **Forget all** — and a **Bench only** control filters
  the list to the batch. Which tag means "on the bench" is the server's contract, sent to the UI
  rather than hard-coded in it.
- **Fleet-wide export (`A23`)** — the whole fleet as one file, from two links on the dashboard: a
  **CSV** with one row per drive, which is what you sort when planning a purchase, and a
  **printable page**, which is what you attach when several drives are going back at once. Both
  render from the same column list and the same rows the dashboard shows, through the same ranking,
  so the export cannot disagree with the screen. Three rules: **an absent reading is an empty cell,
  never a zero** (a blank is what a spreadsheet's `AVERAGE` skips; a `0` drags every average down and
  reads as a measurement), **the export is never filtered** (not by the dashboard's filter, not by
  hidden — a wrong number gets noticed and a missing row does not), and **the rows are in the
  ranking's own order**, because that is the one thing a reader cannot recover from the file.
- **Warranty/RMA evidence pack (`A27`)** — every report format now says what it is for, and names
  the evidence: the attributes failing the drive's **own manufacturer threshold**, plus the counters
  this app reads when it assesses a drive, from the same list the verdict uses. The default text
  format had no attribute table at all, so a report could say `BAD` and name no reason a support
  engineer could check. Selecting by the drive's threshold rather than ours keeps the report from
  contradicting itself: it is a listing, not a verdict, so a threshold changed since the scan cannot
  make the two disagree. It claims evidence, not proof.
- **Config import/export (`C7`)** — one JSON bundle of device labels, settings and schedules, with
  Export and Import in Settings. For a host migration: names, bays and tags live in the database,
  which survives a restart but not a move, and re-labelling sixteen drives by hand is an evening's
  work. **Import merges**, with the file winning for what it contains — a restore that deleted
  anything it did not recognise would be one you could not safely run twice — except schedules,
  which are replaced and say so. **Channels are deliberately excluded**: their config holds tokens
  and passwords, an export file is exactly the thing that gets emailed around, and the app already
  refuses to serve secret values over the API. The bundle carries a note saying so, so a file found
  in a drawer a year later explains itself.
- **"Replace first" ranking (`A24`)** — the dashboard carries a shortlist ordered by how strongly
  each drive argues for replacement, answering "which one do I buy this month?" rather than "how
  does each drive look today". It uses **bands** (replace now / soon / watch) rather than a weighted
  score, so it can show its reasons — "failing now", "7 new sector problems in 30 days", "94% of its
  rated write endurance used" — because a reader can disagree with a band and act on it, and cannot
  do either with an unexplained number. Write endurance can outrank a healthy verdict, since it is a
  countdown rather than a state. The card is absent when nothing is worth replacing, and capped at
  five entries. A disconnected drive is left out: its stored verdict is history, not a reading.
- **Snapshot diff (`A18`)** — a "Since the last scan" card on the device page showing what moved
  between the two most recent scans: verdict, score, temperature, wear, bad sectors, and the
  counters that moved. The server decides which counter movements are findings, using the drive's
  *own* threshold and the lost-storage set the interpretation already sums, so the card cannot
  disagree with the verdict; everything else that moved is counted and named as a count rather than
  dropped. An absent reading renders as an em dash, never a zero, and "nothing changed" is stated in
  words rather than left as an empty panel.
- **Group by tag/pool (`D9`)** — the dashboard can group the list by tag, so a NAS reads as three
  pools instead of twenty-four drives. It is a toggle rather than a filter, offered only when
  something is actually tagged, and a drive with several tags appears under **each** of them —
  first-tag-wins would be tidier but would silently drop a drive from a group it belongs to. The
  header says outright that the counts can add up to more than the fleet size, and untagged drives
  come last. Grouping works in both the card and table views; the table uses a separator row that
  spans every column, so its header stays the only one.
- **Dense fleet table (`D8`)** — a Cards/Table switch on the dashboard. The table gives one row per
  drive with bay, verdict, score, transport, model, capacity, temperature, age and last scan, and
  its headers sort — clicking the sorted column reverses it. A card grid is right for four drives
  and wrong for twenty-four, so the grid stays for small hosts. The list endpoint now carries
  `power_on_hours` and `wear_pct` so a row can show a drive's age without opening it.
- **Filter & sort (`D5`)** — the device list can be ordered by risk, name, bay, model, capacity,
  temperature or last scan, and filtered by transport. Each sort field carries its own direction,
  because "first" means different things per field, and a drive that reports no reading sorts last
  rather than first. The transport filter offers only the transports actually present, and search
  now covers the bay as well as the name, model, serial and mountpoints.
- **Bay / slot field (`D10`)** — a drive can be given a physical location ("bay 4", "front top"),
  which then leads on its card and appears in every export. A serial identifies a drive; a bay
  *finds* it, which is the question someone standing in front of a machine at 3am actually has.
  It is free text, and unset bays are omitted from the card entirely rather than rendered as an
  empty row — a gap in the bookkeeping is not a finding about the drive.
- **Self-test result notifications (`B9`)** — when a self-test finishes, the result is sent to any
  channel subscribed to `self_test_result`, at high priority if it failed. It is the one event that
  reports good news, and it is not opt-in the way the scan heartbeat is: it only ever exists because
  somebody started a test, so it can never arrive unsolicited.
- **Self-test triggering (`A19`)** — the device page offers a short and a long test, shows the
  drive's own progress reading while one runs, and can stop it. This is the first and only control
  in the app that changes a drive, so it is opt-in by construction: nothing starts a test on its
  own, the test kind is an allow-list of two strings, and both actions sit behind a confirmation.
  Progress is the drive's own percentage remaining rather than a figure derived from a clock, and a
  drive that cannot have its self-test log read is told so instead of being offered a button that
  would only fail.
- **Scheduled self-tests (`A19`, second half)** — a schedule action that tests **every drive that can
  take one, one drive at a time**, in serial order. The staggering is the whole feature: twenty-four
  long tests starting together is an I/O meltdown, so the fire *queues* a pass and returns rather than
  running it inline — a pass takes hours, and the scheduler is one goroutine every other schedule
  shares. The test kind is per schedule (`short` or `long`), because a short test is minutes and a long
  one is hours, and that is not a choice to make on somebody's behalf. A drive that is already testing
  itself is skipped rather than asked and refused; a drive whose log cannot be read mid-test is *waited
  for* rather than treated as finished; and a drive that never reports progress gets its own
  `polling_minutes` as a floor before the pass believes the test is over. Each drive is re-read as its
  test finishes, so the result is stored and notified by the same path a manual test uses. Stopping the
  app stops the pass but **not the drive** — hours of work that is doing the right thing is not waste.
  The schedules page says what a running pass is doing, because a `last_run` that has already passed
  tells a reader nothing. See `ADR-024`.
- **Group-scoped channels (`B12`, second half)** — a channel now has two filters, and they answer
  different questions. The event list says *what happened*; a new **group scope** says *to which
  drives*. Tick `tank` and that channel hears about the tank and nothing else, even while a bench
  channel subscribing to the same events hears only about the bench — which is the difference
  between alerts somebody reads and alerts they learn to skip. An empty list is every group, so every
  existing channel behaves exactly as it did. Three decisions are worth naming: a **fleet-level event
  is never filtered** (scope a channel to a pool and it must still tell you that scheduled scans are
  failing); a `group_heat` finding is matched on the group it names, because that is what it is about;
  and **Test ignores the scope**, because the question Test answers is "does this channel work".
  The groups offered are the tags actually in use, and the helper that lists them is now shared with
  the dashboard's export scope — one question, one implementation.
- **Drive logs (`A20`)** — the error log and the self-test log are rendered, with the drive's own
  self-test time estimates and smartctl's words when a log cannot be read.

### Changed

- The self-test card is the one place the UI polls a device: every 15 seconds, while a test it
  started is running and the card is still on screen. `FEATURES.md` §E records this as the single
  documented exception to "UI freshness is tied to the last scan", with the reasoning, and the poll
  stops on both ends — when the drive reports no test running, and when the card leaves the page.

### Changed

- **The projection window follows retention (`A26`).** The longevity estimate, the worsening flag
  and the trend adjustment all looked at a fixed 180 days, so an owner who raised `retention_days`
  to five years precisely to watch a slow trend had that history discarded by the projection anyway.
  All three now use the retention setting itself, so what the user keeps is what the app reads.
  There is one number rather than two, and it is the same one the pruning uses.
- **The health score is now trend-aware (`A17`).** A verdict is a statement about *now*; a score is
  a summary, and a drive whose bad sectors are climbing is worse than one sitting at the same count
  and going nowhere. Such a drive now loses 3 points for its first new sector and one more per
  doubling, capped at 10, with the reason appended to the verdict rationale as a rate and a window
  rather than a date. The cap is what keeps it honest: the trend adjusts the summary, and is too
  small to overrule the drive's own data or carry it out of its verdict on its own. A drive that
  could not be read is not adjusted at all.
- **The trend card's x-axis is time, not reading index** (`ADR-023`). One change gives all three
  things the rework was for: a 30-day window now spans 30 days whether or not anything was recorded
  in them, so a drive that stopped being scanned leaves a *visible gap* rather than a line that
  quietly ends at the right edge; a series that began five days ago occupies its own fifth of the
  window instead of being stretched edge to edge; and a shared date axis under the card makes the
  span readable **without hovering**. The hover marker and the readout are positioned on the same
  time scale as the line, so pointing at a chart now gives the reading nearest the pointer rather
  than one a fraction of the way through the array. Per-metric y-scaling is unchanged.

### Changed

- **The self-test refresh interval is a setting** (`selftest_poll_seconds`, 2–30 seconds, default 15). The
  default was chosen because a long test moves in single digits of percent an hour, so polling faster
  mostly spends device reads — but that is a trade the owner should get to make: a short test on a fast
  drive moves visibly every couple of seconds, and a drive behind a bridge that dislikes extra commands
  would rather be asked less often. It sits with Appearance, and the card's own note says which interval
  is in use.
- **A drive that does not report self-test progress gets the drive's own estimate instead.** Some drives
  only write a self-test log entry when a test *finishes*, so there was no figure to read while one ran
  and the card had nothing to show for a test that was working perfectly well. It now shows the elapsed
  time against the drive's own `polling_minutes`, labelled as the drive's estimate rather than a reading
  from it, and capped short of full — a bar sitting at 100% while a test is still running reads as a
  hang, which is the exact failure a progress figure exists to prevent.
- **The self-test log is its own section inside the drive-logs card**: a heading and a rule, so the
  entries read as the detail under the summary rather than as more rows of the same table.
- **The NVMe endurance slider goes down to 1%.** It bottomed out at 50, which made the setting
  impossible to exercise on anything but a nearly-worn drive.
- **The self-test log dates each run.** The drive records *lifetime hours* rather than a date, and "4d
  ago" was the wrong shape for a log a reader scans for *which run* this was — a relative age moves every
  time the page is opened, and two rows four days apart read the same. Rows now carry an absolute stamp
  ("23 Oct, 09:30") with the drive's own hours beside it, and the tooltip says how the stamp was derived.
- **The settings sheet is a single column of rows.** Two columns of rows turned out to be worse than the
  cards it replaced: a row is a label, an explanation and a control, so one line of the page carried six
  columns of text. The height is bought back where it belongs instead — the longest explanations are one
  or two lines now rather than four, which is most of what made the page tall in the first place.
- **Each self-test now says when it ran.** The log records *lifetime hours*, not a date, and "29,730 h"
  on its own answers a question nobody asked. The history pairs every lifetime-hours reading with the
  time it was taken, so a test is placed on a real clock ("2d ago") with the drive's own figure beside
  it. A test older than the retained history shows its hours rather than a date, because inventing one
  from a rate would be a number nobody could check.
- **The bench card's explainer sits on the title's baseline.** Centring two different text sizes left
  the smaller one floating above the line the eye was following, which reads as a mistake.
- **The settings page is one sheet with sections, not four cards.** Four cards of different heights gave
  a ragged bottom, and reaching the shortest one meant scrolling past the tallest — so the grid did not
  make the page shorter, it made it uneven. Sections in one surface let each section's rows sit in two
  columns, which is where the height saving actually comes from. The knowledge card's paragraph of
  self-explanation is gone with the restructure: the one thing in it that was not visible from the card
  ("every figure names its source") now sits on the heading line.
- **The raw smartctl output has a Copy button.** That output is what people paste into a bug report or a
  forum thread, and a drag-select across a scrolling box is the thing the button exists to replace. It
  falls back to selecting the text when `navigator.clipboard` is unavailable, which on a self-hosted app
  over plain http it usually is — a Copy button that silently does nothing is worse than no button.
- **The fleet export is one button, not two links.** A dialog carries the format and the scope, which is
  where each format can say what it is *for* — beside the choice it belongs to rather than in a tooltip
  nobody hovers. Scope is a tag, deliberately not the dashboard's filter: a filter is about what you are
  *looking at*, and an export that silently honoured it would hand you a file missing rows you never
  chose to exclude.
- **The sort direction says what it is doing.** A bare ↑ in a ghost button beside a dropdown read as
  decoration rather than as the other half of the sort; it is a real arrow and the word now, in a
  labelled pair with the field it belongs to. The transport filter has its own label too, offers "All"
  rather than "All transports", and orders by kind rather than alphabetically so the control keeps its
  shape as drives come and go.
- **A drive joins the bench with a checkbox.** You had to know the word `bench` to find the feature at
  all; the Notes dialog now carries **Being tested — left out of the fleet counts**, kept in step with
  the tags field in both directions. The card is now **Drives in testing** with its explainer on the
  title line rather than in a paragraph of its own, names its drives as chips rather than one row each,
  and says what the state *means* instead of what the tag is called.
- **The dashboard header is three rows instead of five.** The notes that explain the counts now sit
  beside them on one muted line rather than in rows of their own, and the standing instructions about
  rescanning and forgetting are gone — a line of help on every page load is read once and then only
  takes up room. The view switch became a segmented pair, and a rule separates it from the two export
  links: "how is this shown" and "what happens to this list" are different questions, and shoulder to
  shoulder they read as one group.
- **The trend card's date axis is legible, and carries up through the plots.** It was 12px muted text
  under the card; it is a step larger now, on tabular figures, with a rule tying it to the plots — and
  the same three positions are drawn as faint vertical rules through every sparkline, so a date can be
  followed to a line rather than guessed at from a label at the bottom.
- **The outlook caveat is one line.** It ran to three, and a paragraph above a figure stops being read.
  The rating's provenance is not lost with it: the knowledge base names the source of the rating beside
  the number it divides by.
- **The schedule editor says where a digest goes.** It said what a digest *is*; the question left open
  was where it is sent — your notification channels, because the schedule is the subscription. The hint
  appears only when the action is a digest.
- **An exported report opens on the drive, not on the app.** The purpose and the provenance bracketed
  the content: three lines about the document above the drive's identity and two more below the last
  table, which is roughly eighty words of prose about the app around the data the file exists to carry.
  Both facts are kept and both now sit together at the **foot** of every format — under a rule in the
  text report, in the footer of the page, and as the last two rows of the spreadsheet — at about half
  the length. Nothing above the serial number but the serial number.

### Fixed

- **The JS test DOM stub had no `querySelector`, so the channel editor's whole save path was
  untestable.** The editor reads its own checkbox state back out of the DOM to build a save payload,
  which threw a `TypeError` under the stub — and the editor's own `catch` turned that into a "Save
  failed" toast, so a test could only ever have seen a form that looked like it worked and a request
  that never left. Found by the first test that asserted on a payload rather than on the rendered
  result. The stub now implements the slice the views use.
- **The self-test card's baseline is read from the drive, not from the last scan.** It was comparing the
  live log against the copy the last scan recorded — which is stale the moment a test has run since — so
  the first look after starting a test saw a "new" row that was not new, concluded the test was already
  over, and skipped the in-progress state entirely. That is why no progress appeared and why the result
  only showed up after a scan: the card never entered the state that polls for it.
- **A self-test's progress is also read from the drive's own self-test-execution status.** The ATA spec
  puts the percentage in the status byte of the SMART data structure, and drives differ about whether
  they report a running test there, in the self-test log, or in both. Reading only the log left such a
  drive looking idle mid-test. Both are read now, and the status field's *last outcome* is not mistaken
  for a running test: the signal is the percentage, and that is only present while one executes.
- **A drive already running a self-test is no longer asked to start a second one.** The card reads the
  drive before asking it to do anything, so "this drive is already running a self-test" replaces the
  drive's own refusal — which on an NVMe drive arrives as a bare exit status.
- **A finished self-test's result reaches the page without a scan.** The drive-logs panel is drawn from
  the last scan, so a test that had just finished existed nowhere on screen until the whole fleet was
  re-read — which made one test's outcome look like it was tied to the big Scan button. The self-test
  card already reads the drive *live* while a test runs, so it hands that log over and the panel redraws
  from it. A test that finished on a drive which never reported it *running* is noticed the same way: a
  new row in the drive's own log is the evidence, and the drive wrote it.
- **A drive whose self-test log is the *standard* variant reported no self-test log at all.** smartctl
  nests the ATA self-test log under `standard` (the shorter log) or `extended`, and the parser only knew
  `extended` and `summary` — so such a drive showed no count, no entries, and no sign of the test it was
  running, which meant the card offered the run buttons again for a drive already testing itself. That is
  the likely explanation for a SATA drive where a test "does not appear in the history at all".
- **A threshold change now says that it needs a scan.** A verdict is worked out when a drive is *read*,
  and the stored reading is what every page shows — so lowering a threshold changes nothing until the
  next scan, and the settings page said "applied immediately", which was exactly the wrong thing to tell
  somebody who had just lowered one and was waiting for an alert. The page says so now, and offers
  **Scan now and re-check** beside the thresholds.
- **A numeric table heading now lines up with its column.** `td.num` was right-aligned and `th.num` was
  not, so every numeric column had its heading sitting somewhere to the left of its figures, and the
  reader had to check whether a number belonged to the column they thought it did. The ATA attribute
  table had the same bug with four columns of it.
- **A self-test the drive does not report no longer looks like nothing happened — and no longer jumps the
  page.** A drive that does not report progress while a test runs is indistinguishable from one where the
  button did nothing, so the card fell back to its idle state and the app fired a rescan on that guess —
  which re-reads every drive and re-draws the page, which is what made the short test throw the reader to
  the top. It now says what it can see ("Started. This drive is not reporting progress…"), keeps asking
  every 15 seconds, offers **Rescan for the result**, and keeps **Stop test** available. The automatic
  rescan fires only on a running → not-running transition, which is evidence rather than a guess.
- **NVMe self-tests now show progress and results, and the run buttons stop inviting a refusal.** The
  NVMe self-test log has a different shape from the ATA one in the one way that mattered: the test *in
  progress* is a **header field** rather than a table row, and its progress is a separate number. A
  parser that knew only the ATA log reported "no test running" for a drive that was mid-test — so the
  card offered the run buttons again, and the drive refused the second one with a bare exit status.
  That is the whole of "it looks successful but every test after that fails". Both logs are read now,
  against a real WDC SN730 capture.
- **A self-test log that cannot be read is no longer reported as a drive that cannot test itself.** The
  same SN730 under smartctl 7.4 reported "Read Self-test Log failed" and under 7.5, with no hardware
  change, reads the log fine — so concluding "this drive cannot run self-tests" from a failed *read*
  was a confident statement about the wrong thing, and it told the reader to give up on a drive that
  tests itself perfectly well. The unreadable case says exactly that, the card still offers the
  buttons, and only a drive that *says* it does not implement self-tests is reported as unable to.
- **A refused long test no longer leaves a bare exit code.** `smartctl -t long` on a drive that does not
  implement an extended self-test failed with "smartctl exit status 4" and nothing else, because
  smartctl prints no message when a drive declines silently. The failure now names the bit in
  smartctl's own vocabulary ("a SMART command failed"), passes the drive's words through when there are
  any, and says outright when there are none — and the card disables the long button and explains,
  rather than inviting a second attempt at something the drive has already declined. The short test
  stays available, because a refused extended test says nothing about it. It is disabled for the life
  of the card rather than remembered: "refused once" is not the same fact as "cannot", and nothing
  smartctl reports in advance says which tests a drive supports.
- **A self-test's result could vanish.** The result lives in the Drive logs card, which is drawn from
  the *last scan* — and a fifteen-second poll only ever sees a short test after it has finished. So a
  test that was over before the card's first reading never triggered the rescan that puts the outcome
  into the stored log, and the result reached the page only if the user thought to press rescan
  themselves. The rescan now also fires for a test the card *started* but never saw running, and the
  card shows the **last finished result** itself rather than leaving it only in the log table above.
- **A self-test log this app cannot read is now named rather than silently empty.** Progress comes
  from the ATA self-test log; smartctl's NVMe self-test log has never been observed on this project's
  hardware, so it is *detected* (`self_test_log_unread`) rather than guessed at, and the card says
  that progress and past results cannot be shown on this drive. That is a different statement from
  "no test is running", and the only one of the two a reader can act on.
- **A button worked once, then went dead.** Every async click handler that re-enabled its own button in
  a `finally` was reading `event.currentTarget`, which the DOM nulls the moment dispatch finishes — so
  the line threw a `TypeError` into an unhandled promise, silently, because nobody awaits a click
  handler, and the button stayed disabled for the rest of the session. The visible symptoms were "Save
  settings" working exactly once and the channel editor allowing a single Test, but it was eight
  handlers: Save settings, Save notes, Save schedule, Save channel, Test, Apply & rescan, Run self-test
  and Add maintenance window. All eight now capture the element as the handler's first statement. Why
  the tests never caught it is worth recording: the DOM stub handed out a plain `currentTarget` that
  stayed valid forever. It is now a getter that is nulled when dispatch ends, so this bug class is
  caught by the suite rather than by the user — and the stub's behaviour is pinned by a test of its own,
  so a future tidy-up cannot quietly re-blind it.
- **A refresh no longer throws you to the top of the page.** Every view empties its container and *then*
  awaits its data, and a page that is briefly short makes the browser clamp the scroll position to the
  top — which is what "Refresh now" on the knowledge card, a rescan from a device card, and a scan
  finishing in the background all did. The container is now held at its old height across the swap, so
  the position stays valid and only a genuinely shorter page moves the reader. Navigation still starts at
  the top, because a page you have just asked for is one you have not read yet. The trend-range control
  on the device page had been fixed on its own earlier; this is the general fix, so it cannot come back
  on the next card that loads its own data.
- **The cards/table choice survives a reload.** It was module state, so a reader who wanted the table got
  cards again on every refresh. It is now the `dashboard_view` setting — a display preference like the
  theme and the temperature unit, read before the first render so the list never flashes the wrong shape,
  and carried in the config export. The filter and the sort beside it stay in the module: those are about
  the question being asked, not about how this person likes to read.
- **The channel editor no longer explains what its own Test button does.** The paragraph above the form
  said what the button's tooltip already said, and a sentence of reassurance above a form is read once
  and then only seen.
- **A channel with nothing ticked no longer reads as a channel that will never speak.** The card said
  "No events subscribed", which describes the *setting* rather than its effect: an empty subscription is
  every event, because the list records what has been narrowed away and nothing listed means nothing
  excluded. The card now reads **Everything** and names the two exceptions that do have to be ticked —
  the scheduled-scan heartbeat and the group-heat finding — and the editor's hint names both, where it
  had named only the heartbeat. The case is not rare, which is what made the wording costly: it is
  exactly the channel a digest is set up on, and the digest arrives *because* nothing was narrowed away.
- **A modal no longer vanishes mid-edit, and a form no longer closes on a stray click.** Two causes, one
  symptom. A drag that began on a field and ended outside the dialog produced a `click` whose target was
  the dialog (the nearest common ancestor of press and release) at coordinates outside its box — which is
  exactly what a backdrop click looks like, so selecting text in the label field closed the schedule
  editor out from under a half-filled form. Dismissal now turns on where the **press** landed, not where
  the release did. And the editors — schedule, channel, maintenance window, device notes, acknowledge —
  no longer close on a click outside them at all: only **Cancel** and `Esc` do, because a stray click
  beside a form should cost the click rather than the form. Confirmations and the export picker still
  dismiss on a backdrop click, where there is nothing to lose.

### Documentation

- **`docs/TESTING.md` is a test plan now, not a checklist.** Every test is a numbered procedure with the
  steps and the expected result, because "check the dashboard" is not a reproducible instruction — and a
  first pass through this app misses most of it, since a large part of the UI is conditional and a card
  that is correctly absent looks exactly like a card that is broken. Pass 0 builds the data that turns
  the conditions on and the later passes depend on it, so findings can be quoted by number ("3.5, on the
  SN730: progress appeared but the percentage stayed at 0") and the procedure is the bug report.
- **The known NVMe self-test gap is written down where it will be found** (`FEATURES.md` §A19 and the
  plan's Pass 3): progress comes from the *ATA* self-test log, and smartctl's NVMe self-test log has
  never been observed on this project's hardware. It is detected and named rather than guessed at, and
  the raw output from a drive while a test is running is the fixture that would close it.

## [1.0.1] — 2026-10-18

Four fixes found by using the first release in anger, plus the documentation corrections that
came out of deploying it.

### Fixed

- **The attribute `?` dialog rendered the literal word "null"** on every field with no "how to
  read it" line. The native `Element.append` stringifies a `null` argument into a text node, and
  one call site had bypassed the project's own helper that filters them out.
- **Changing the trend window threw the page back to the top.** The re-render mounted a one-line
  skeleton first, which collapsed the page height, so the browser clamped the scroll position to
  zero.
- **Rows smartctl cannot name** read `Unknown_Attribute`, which looks like a read failure. They
  are labelled "Vendor specific" now, with a note under the table explaining that smartctl does
  not recognise the drive model and the counters are vendor-private.
- **The drive-logs panel** said the entries were "in the raw output" without saying where. It
  names the sections: `ata_smart_error_log`, `ata_smart_self_test_log`,
  `nvme_error_information_log`.

### Changed

- Em dashes removed from 31 user-facing strings, every attribute tooltip included.
- The Alpine base tag is no longer named in prose. Dependabot bumps it, and a version written
  into documentation goes stale on the next bump.
- Dependencies: `alpine` 3.20 → 3.24, `docker/metadata-action` v5 → v6.

### Documentation

- **The idle footprint is measured, not a target**: 0.00% CPU and 13.36 MiB RSS on a
  long-running instance with no browser open, inside the "near zero" and "under 20 MB" the MVP
  was written against.
- **Native AHCI/SATA is verified.** A second host exposes a SATA SSD on a native controller,
  closing the caveat that SATA had only ever been reached through a USB bridge.
- **Image size published**: 14.8 MB content, 53.5 MB on disk. The withdrawn "well under 20 MB"
  claim was true of the compressed figure and wrong about everything else.
- A drive with a populated error log was finally captured, and it justifies the counts-only
  decision in the drive-logs panel: on a drive smartctl does not recognise, a logged error
  carries an error number and an index and nothing else.

## [1.0.0] — 2026-10-17

First public release.

### Added

- **Scanning** — on-demand and scheduled, over every block device the host exposes,
  including USB-attached drives. Live progress over SSE, interval or cron schedules, and a
  scheduler that lives inside the container with no host cron.
- **Health reporting** — CrystalDiskInfo-style Good / Caution / Bad verdicts with a
  plain-language rationale, a 0–100 score with a penalty breakdown, and the full attribute
  table with a `?` on every interpreted attribute explaining what it means and how to read
  it. A row the drive *itself* reports at or below its own threshold is marked.
- **History and trending** — per-metric sparklines over 7 / 30 / 90 days or all time, a
  remaining-longevity estimate projected from the drive's own wear counter with its
  confidence and caveat shown inline, and a *Worsening* badge that fires when bad sectors are
  accumulating.
- **Drive logs** — how many entries the error log and self-test log hold, and the drive's own
  estimate of how long its self-tests run.
- **Identity that survives hardware churn** — names, tags and notes are keyed by serial
  number, so they follow a drive across reboots, replugs and device-letter changes.
- **Diagnostics** — a raw `smartctl` output viewer per drive, and a per-device `-d` transport
  override for USB bridges that need one.
- **Notifications** — ntfy, Gotify, Telegram, Discord, SMTP email and generic webhooks, with
  health-transition events, squelching, maintenance windows, and per-channel event
  subscriptions.
- **Per-channel environment overrides** — any channel field can be supplied from the
  environment or a mounted file (`SMARTSERVE_CHANNEL_<LABEL>_<FIELD>`, with a `_FILE` form), so
  a notification token can stay out of the data volume and out of its backups. The editor shows
  the variable names and badges the fields the deployment supplies.
- **Export** — plain text (the format to attach to a warranty claim), a self-contained web
  page, a spreadsheet, or JSON.
- **Interface** — dark, light and auto themes, a custom accent, eleven community palettes,
  a responsive layout, and a health summary that doubles as a filter.
- **Deployment** — multi-arch image (amd64 / arm64), `/healthz` for Docker `HEALTHCHECK`,
  and `SMARTSERVE_BASE_PATH` for subpath hosting behind a reverse proxy.

### Security

- **No authentication is built in, by design** (`ADR-016`). Smart-Serve expects a reverse
  proxy to provide it. Do not expose it directly to an untrusted network.
- The container needs read access to raw device nodes, which requires `CAP_SYS_ADMIN` and
  `CAP_SYS_RAWIO`. That is a real privilege grant — read `docs/DEPLOYMENT.md` before
  deploying, and prefer the least-privilege option that works for your host.
- Notification channel credentials can be supplied from the environment or a mounted file
  (`SMARTSERVE_CHANNEL_<LABEL>_<FIELD>`, with a `_FILE` form), so a token need not live in the
  `/data` volume or in whatever backs it up. A `_FILE` that cannot be read fails the send and
  records why, rather than falling back to a stored value (`ADR-022`).

### Known limitations

- SMART self-tests are not yet triggerable from the UI (`FEATURES.md` A19).
- Error-log *entry* lists are not displayed — only their counts. No drive available to the
  project has ever logged an error, so the populated shape has never been observed
  (`FEATURES.md` A20).
- Native AHCI/SATA controller access is untested; SATA drives have only been exercised
  through a USB bridge.
- The idle-footprint figures are targets, not measurements, pending a run on a public
  instance.

[Unreleased]: https://github.com/masieono/smart-serve/compare/v1.0.1...HEAD
[1.0.1]: https://github.com/masieono/smart-serve/releases/tag/v1.0.1
[1.0.0]: https://github.com/masieono/smart-serve/releases/tag/v1.0.0
