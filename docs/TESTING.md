# Smart-Serve — Manual test checklist

`go test ./...` and `node --test internal/webui/jstest/*.test.mjs` cover the rules. This file covers
what a machine cannot: whether the thing that *should* appear appears at all, whether it says the
right thing, and whether it is where you would look for it.

It exists because a first pass through this app misses a lot, and mostly for one reason — **a large
part of the UI is conditional**. A card that is correctly absent on your fleet looks identical to a
card that is broken, and the only way to tell them apart is to know what turns it on. Every entry
below marked **(conditional)** says what it needs.

## Before you start: build the test data

Most of what is missing from a first pass is missing because nothing has triggered it. Set this up
first, or you will spend the pass looking at empty states.

| What you need | What it turns on |
|---|---|
| A drive tagged `bench` (the tag name is a setting the server owns) | the **Testing session** card, the **Bench only** filter, the batch export |
| Two tags, with one drive carrying both | **Group by tag**, and the "appears under each of them" note |
| A bay recorded on two drives | the Bay column, the Bay sort, the bay row on a card |
| A drive at **caution** | **Acknowledge** / **Un-acknowledge**, the **Replace first** shortlist, a digest at medium priority |
| A drive at **bad** | the summary counts, Replace first, the digest's "Replace first" section |
| A drive that is **disconnected** | the **Show N disconnected** toggle, the "Not connected" card |
| A drive with **no S.M.A.R.T.** | the blocked-read / no-SMART notice and its compose snippet |
| **Two or more scans**, hours apart | the **Trend** card, **Since the last scan**, **Outlook** |
| **4+ drives in one tag group**, warmed together | the **Heating together** card |
| At least one **notification channel** | the digest, the delivery log, maintenance windows |

## Dashboard

- **Summary counts** filter the grid when clicked, and the filter survives navigating into a drive
  and back. **(conditional)** The counts only render where there is a fleet.
- **Search** — name, model, serial, device path, bay and mountpoint. `/` focuses it.
- **Sort** — every field in the dropdown, and the direction toggle. Check the *default direction*
  per field: capacity, temperature, age and last scan read as "biggest/hottest/oldest first", the
  rest as A→Z.
- **Table headers** sort too; clicking the sorted column reverses it.
- **Cards / Table** — switch it, then **hard-refresh**: the table should still be the table. This is
  stored in the database (`dashboard_view`), so it also survives a different browser.
- **Group by tag** **(conditional — needs a tag)** — untagged drives come last, and a multi-tag drive
  appears under each of its tags with the note explaining why the counts do not add up.
- **Show N disconnected** **(conditional)** and **Bench only** **(conditional)**.
- **Replace first** **(conditional — needs a drive that qualifies)** — capped at five, absent when
  nothing does. It should never appear just because the fleet is large.
- **Heating together** **(conditional — needs 4+ drives in one group rising together)** — check the
  ratio (`3 of 4`), and that it never claims a cause.
- **Testing session** **(conditional — needs a `bench`-tagged drive)** — `Test all`, `Export batch`
  (a tag-scoped CSV) and `Forget all`, plus the note about the batch being left out of the counts.
- **Export CSV** and **Export page** **(conditional — needs more than one drive)** — at the far end of
  the controls row, past the rule.
- **Rescan a card**, and confirm the page does not jump to the top when it finishes.
- **The whole header should be three rows at most** (identity, controls, one note line) and two when
  nothing conditional is on.
- Empty state on a host with no drives discovered.

## Device page

- **Verdict, score and grade**, and the rationale behind them.
- **Knowledge base** **(conditional — needs a drive the table knows)** — the rated endurance line and
  its source, and an advisory card when the firmware matches. See `KNOWLEDGE.md`.
- **Since the last scan** **(conditional — needs two scans)** — including "nothing changed" and an
  em dash for a value the drive did not report.
- **Trend** **(conditional — needs history)** — the 7/30/90/all windows, the shared date axis, and the
  faint rules through each plot. Hover and arrow keys. A drive that stopped being scanned should
  leave a **visible gap**, not a line that ends at the right edge.
- **Outlook** — the caveat is one line now, and the "Worsening" badge appears only when it is.
- **Acknowledge / Un-acknowledge** **(conditional — needs a caution or bad drive)**. Ack it, then let
  it get worse: the ack is spent the moment the verdict moves.
- **Self-test** — the short and long buttons, the confirmation, the estimate. Then:
  - a test that is **running** shows a percentage and a **Stop test** button;
  - a test that **finished before the first reading** says so, and the card picks the result up;
  - **Last test** shows the most recent finished result, with the drive's own verdict;
  - a drive that **cannot self-test** says so instead of offering buttons.
- **Drive logs** — both counts, and the self-test table when there is one.
- **Attribute table** — the `?` help, flagged rows, `Unknown_Attribute` → "vendor specific", and the
  NVMe table for an NVMe drive.
- **Raw output** viewer, and the transport override + **Apply & rescan**.
- **Notes / name / bay / tags / hidden** — save, and confirm the page reflects it. Save **twice**.
- **Report export** in all four formats.
- **Forget this drive** — behind a confirmation, and it returns to the dashboard.
- The three degraded cases: **no S.M.A.R.T.**, a **blocked read**, and a **disconnected** drive.

## Schedules

- The guided interval / daily / weekly / cron forms, the cron preview, and the validation messages.
- **Action: digest** — the hint should say where the digest *goes* (your notification channels; the
  schedule is the subscription). Pick scan and confirm the hint disappears.
- **Run now**, enable/disable, edit, delete.
- **Save twice** — the button must come back.

## Notifications

- Channel types: ntfy, Gotify, Telegram, Discord, email, webhook. Event subscription checkboxes.
- **Test in the editor** (unsaved settings) — press it **three times**. Then **Save twice**.
- **Test on a saved channel** in the list.
- **Delivery log**, and the maintenance-window card: a one-off window, a daily one, a weekly one, and
  one scoped to specific drives. **(conditional)** The card is absent when there are no windows.

## Settings

- **Save twice.** Then change the temperature unit with unsaved edits in the fields and confirm
  nothing typed is reinterpreted.
- **Knowledge base card** — `Refresh now` should not move the page. `Download the shipped table`,
  the overrides editor (paste broken JSON and read the error), and `Discard the fetched copy`.
- **Import / export** — export, then import the same file. Try a malformed file for the error text.

## Cross-cutting

- Theme and temperature unit persist, with no flash of the wrong one on load.
- `s` scans, `/` focuses search, `?` shows help, `Esc` closes a dialog.
- Narrow window: the sidebar collapses, the topbar toggle works, Scan stays reachable.
- Behind a subpath (`/smart-serve/`) — nothing should 404.
- **Nothing jumps the page**: refresh a card, rescan a drive, let a scan finish in the background.
- No polling anywhere except the self-test card while a test it started is running and on screen.

## When something is missing

Check this order before calling it a bug:

1. **Is it conditional?** Almost everything in this file is. The trigger is named beside it.
2. **Is the data stale?** The device page is drawn from the last scan. A value that changed since
   then appears after a rescan — that is the design, not a bug (`FEATURES.md` §E).
3. **Did the drive report it?** A blank where a value should be is often the drive not reporting it.
   The app renders those as an em dash on purpose, and `raw output` will show you what it sent.

