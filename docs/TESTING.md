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

## Pass 7 — cross-cutting

| # | Do | Expect |
|---|---|---|
| 7.1 | Reload after setting a theme and a unit. | No flash of the wrong theme or the wrong unit |
| 7.2 | Press `s`, `/`, `?`, `Esc`. | Scan; focus the filter; help; close a dialog |
| 7.3 | Narrow the window below 640px. | The sidebar collapses, the toggle works, Scan stays reachable |
| 7.4 | Let a scan finish while you are reading a drive page. | A toast, and **the page does not move** |
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

