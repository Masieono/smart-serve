# Smart-Serve — UI / UX Design

This document exists because a clean, sleek, easily-navigable interface is a
**first-class requirement**, not a finish line. It is intentionally prescriptive so
that implementation cannot drift into a generic default look.

> **Implementation status:** the interface is implemented under `internal/webui`
> (embedded assets + server-rendered theme). The component inventory (§6), screen
> specs (§7) and acceptance criteria (§11) below are the contract it was built to.

## 1. Design principles

1. **Glanceable first.** A drive's overall health must be readable in under two
   seconds from the dashboard. Verdict + score + temperature are the headline.
2. **Calm and clean.** Generous whitespace, restrained colour. Colour carries
   *meaning* (health state) and is never decoration.
3. **CrystalDiskInfo's data density, modernised.** Power users get the full attribute
   table; everyone else gets a clear verdict. Progressive disclosure, not clutter.
4. **Honest freshness.** Every screen shows "last scanned …". The UI never implies
   live monitoring — it reflects the last scan (`ADR-007`).
5. **No framework, no CDN, no external fonts.** System font stack + inline SVG icons
   + CSS custom properties. Fast, offline-capable, and immune to framework/version
   churn — the single biggest cause of UI regressions in iterative development.
6. **Every state is designed.** Empty, loading, scanning, error, and populated states
   are all specified (Section 7) — a blank screen is a bug.

## 2. Information architecture & navigation

A single-page shell with five top-level views:

| View | Purpose |
|---|---|
| **Dashboard** | Grid of device cards + health summary bar + global Scan button |
| **Device detail** | Full report for one drive (drill-in from a card) |
| **Schedules** | Manage scheduled scans (interval / cron) |
| **Notifications** | Manage channels (ntfy/webhook), event subscriptions, delivery log |
| **Settings** | Thresholds, scan options, theme picker, about/version |

Persistent global elements: **Scan button** (primary action, always reachable),
**theme picker**, and the **health summary bar**.

- Desktop (≥1024px): fixed left sidebar navigation + content area.
- Tablet (640–1023px): collapsible sidebar (hamburger).
- Mobile (<640px): single column, sticky top bar with the Scan button.
- The sidebar's **wordmark is the way home**, as a wordmark in a corner is expected to be.
  It is a `<button>` rather than an `<a>` because every navigation in this shell goes
  through `ctx.navigate`, which owns the base path — a raw `href` would have to interpolate
  it and would get it wrong behind a proxy (`ADR-015`).

URL routing uses hash or path segments (`/`, `/device/{serial}`, `/schedules`, …) so
views are bookmarkable and the browser back button works.

## 3. Layout & responsive behaviour

- Mobile-first CSS with three breakpoints (640 / 1024 / 1440px).
- Device cards in a responsive grid: `repeat(auto-fit, minmax(min(100%, 320px), 1fr))`.
  `auto-fit` rather than `auto-fill`, because `auto-fill` reserves the empty tracks:
  four drives on a wide screen would occupy the first four of seven columns and leave
  a third of the row blank. `auto-fit` collapses the unused tracks, so cards fill the
  row and only wrap when they run out of room.
- **No maximum content width.** Data views (the device grid, tables, the delivery log)
  use the full window. A capped column wastes most of a wide screen for no benefit.
  Controls that would look absurd stretched are constrained individually
  (`--form-max`), not by capping every page.
- The topbar holds only the mobile navigation toggle; on desktop the sidebar is always
  visible, so the bar is hidden rather than left as an empty strip.
- The attribute table is horizontally scrollable on narrow screens rather than
  collapsing into unreadable stacked cells.
- Touch targets ≥44px on mobile.

## 4. Design tokens

All styling flows from tokens defined once as CSS custom properties. **Components
never hard-code a colour, radius, or spacing value** — they reference tokens. This is
what makes theming (Section 5) a token-swap rather than a rewrite.

- **Spacing scale:** 4, 8, 12, 16, 24, 32, 48 px.
- **Type scale:** 12 / 14 / 16 / 20 / 24 / 32 px; system font stack
  (`system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`); monospace stack for
  attribute values and serials.
- **Radii:** 6 (controls), 10 (cards), 16 (dialogs).
- **Elevation:** two shadow levels (card, dialog).
- **Semantic colour tokens:** `--bg`, `--surface`, `--surface-2`, `--text`,
  `--muted`, `--border`, `--accent`, `--accent-contrast`, `--good`, `--caution`,
  `--bad`, `--unknown`, `--info`, `--focus`.
- **Data-viz tokens:** `--gauge-track`, `--gauge-fill`, `--sparkline` (separate from
  semantic colours so charts stay readable in every theme).

## 5. Theme system

Theming is a **token-swap**, never a component change. This is the key to shipping a
theme picker without touching any component code.

- **Mechanism:** each theme is a `[data-theme="…"]` block setting the `--palette-*`
  tokens; the semantic tokens (`--bg`, `--surface`, `--text`, …) derive from those,
  and components only ever reference `var(--…)`.
- **Built-in themes:** `auto` (follows the OS via `prefers-color-scheme`), `light`,
  `dark`.
- **Named palettes:** `catppuccin-latte`, `catppuccin-frappe`, `catppuccin-macchiato`,
  `catppuccin-mocha`, `dracula`, `nord`, `gruvbox-light`, `gruvbox-dark`,
  `synthwave-light`, `synthwave-dark`, `hacker`, `solarized-light`, `solarized-dark`,
  `tokyo-night`, `one-dark`. Each states its own `--palette-*`, accent and health
  colours, so a palette looks like itself rather than the default in a different
  shade. Their soft variants and hover shades are mixed once from the solid values
  (`color-mix`), so a new palette is a dozen lines plus a line in the derived-token
  `:is(...)` rule.
- **Custom theme:** the user picks an **accent colour** and a **base** (light or
  dark). Derived tokens (`--accent-contrast`, hover/active shades, focus ring) are
  computed from the accent's luminance so text on the accent always meets WCAG AA
  contrast automatically.
- **The accent picker belongs to `custom`.** A named palette owns its accent; the
  server emits no inline override for one, and `applyTheme` clears any override left
  behind when switching away. An inline value wins over the stylesheet, so leaving
  one in place would flatten every theme to the same colour.
- **Surface tint** (`neutral` / `slate` / `warm` / `cool`) remixes whichever palette
  is active, so it composes with a named theme.
- **No theme files:** a theme is a handful of token values stored in settings —
  nothing to install, nothing to break.
- **No flash of wrong theme:** the server renders `<html data-theme="…">` from the
  stored setting, so the correct theme is applied before first paint (no JS-only
  theming). A tiny inline script only handles `auto` → OS resolution.
- **Persistence:** `theme`, `theme_base`, `theme_accent`, `theme_surface_tint`
  settings (`DATA_MODEL.md`).

Two tests keep the picker and the stylesheet honest: the Go side asserts every name
in `ThemeNames` has a palette block in `tokens.css`, and the JS side asserts the
picker offers exactly `ThemeNames`.

## 6. Component inventory

| Component | Notes |
|---|---|
| **Verdict badge** | Pill: icon + word + colour (`good`/`caution`/`bad`/`unknown`). Colour is never the only signal. |
| **Health gauge** | Circular SVG ring, 0–100 + grade. Colour = band. Sized from the `size` argument, never from CSS — a bare `svg` rule would override its width/height attributes. |
| **Device card** | Name (user), model, capacity, verdict badge, small gauge, temperature, transport icon (USB vs internal), mountpoint(s), "last scanned". Actions: open, scan-this, hide. A card whose drive is absent from the latest scan shows a *Not connected* badge and is dimmed; a drive we were not allowed to read shows *Blocked*; a drive that cannot report S.M.A.R.T. shows *No S.M.A.R.T.* — and neither draws a gauge. |
| **Devices header** | One card holding the page title, the clickable verdict counts and the search box. The counts are a filter for the grid below, so they sit beside the thing they filter instead of in a row of their own. |
| **Attribute table** | Zebra rows; ID / name / value / worst / threshold / interpreted raw. Every row the interpreter acts on carries a `?` that opens an explanation of what the field is and how to read it, and a row is tinted and badged *At threshold* when the drive reports it at or below its own threshold (`FEATURES.md` A12). Not sortable — the table is short enough to read, and sorting it is not worth a control. |
| **Sparkline / trend card** | Inline SVG per metric (health score, temperature, power-on hours, wear) with the current value and the change across the window, plus a 7 / 30 / 90 day / all range selector. Hovering (or arrowing, when focused) shows a marker and a three-line readout — value, date, time — for that reading. A row with fewer than two readings is dropped — one point is not a trend. |
| **Notice** | A neutral panel for an explanation that is not a fault. Two of them: "this drive does not report S.M.A.R.T." (with the transport-override control), and — added later — "the container was not allowed to read this drive", which names which of the three access gates is closed and prints the compose fragment that opens it. Deliberately not an error box in either case: neither is the drive failing. |
| **Export dialog** | One row per format (plain text / web page / spreadsheet / JSON) with what each is for, rather than a single button that silently picks one. |
| **Panel grid** (`.panel-grid`) | The shared page layout, shared by the settings page and the device detail page: `repeat(auto-fit, minmax(min(100%, 460px), 1fr))`, so the settings page's three cards sit side by side at wide widths. One grid per page, never one per row: two separate grids size their columns independently and their gutters drift apart. Cards are stretched to their row rather than `align-items: start`, so the gap beneath a short card matches the gap beside it. `.panel-wide` spans both columns, for prose like the notes card. |
| **Panel grid, capped** (`.panel-grid-pair`) | `.panel-grid` capped at two columns however wide the viewport gets — one below 1024px, two above. Auto-fit fits *four* 460px panels across a 2560px monitor, which squeezed the trend sparklines and the health-log table into columns too narrow for the type they carry. The device detail page uses this; settings keeps auto-fit, because three short cards genuinely do fit. |
| **Scan button** | Primary action. States: idle · scanning (spinner + "3/5 devices") · done (toast). |
| **Toasts** | Scan complete, saved, notification sent; auto-dismiss, stacked. |
| **Dialogs / sheets** | Schedule editor, channel editor, maintenance window, device notes, acknowledge, export, confirmations. Full-screen sheets on mobile. A dialog holding unsaved input — every editor above — does **not** close on a click outside it; only **Cancel** and `Esc` do, because a stray click beside a half-filled form should cost the click rather than the form. Confirmations and the export picker still dismiss on a backdrop click, where there is nothing to lose. Dismissal turns on where the *press* landed, not where the release did: a drag that starts on a field and ends outside the box is a text selection, and it used to close the schedule editor mid-edit. |
| **Empty states** | "No scan yet — press Scan to discover drives", "No devices found", "No schedules", "No history yet — trends appear once this drive has been scanned a few times". |
| **Skeletons** | Placeholder shapes while data loads — never a blank screen. |
| **Settings forms** | Inline validation; each field shows its **effective value, default, and source** (`env` or `db`, per `ADR-009`). |

## 7. Screen specifications

### 7.1 Dashboard
- Summary bar (verdict counts) → device grid.
- **Scan actions:** exactly two — the sidebar's global **Scan now**, and a rescan on
  every card. There is deliberately no header copy of the global action.
- **Empty:** onboarding message + prominent Scan button.
- **Loading:** skeleton cards.
- **Scanning:** the sidebar button reflects progress; cards update live via SSE as
  each device completes (no polling — `ADR-006`).
- **Populated:** cards sorted by severity (bad → caution → good), then by user name.
- **Sorting and filtering** (`FEATURES.md` D5): a second row beneath the identity row, drawn only
  when there is more than one drive to order. The sort field carries its own direction — risk and
  name ascending, capacity and temperature descending — because "first" means different things per
  field, and one global toggle would make the control feel broken on first use whichever way it was
  set. Ties break on name, so a rack of identical drives does not reshuffle between renders. The
  transport filter is built from the transports *actually present*, not from a vocabulary the app
  invented: a host with no SAS drives should not be offered "SAS" and then show nothing when it is
  picked. Search covers the bay, because hunting a drive by where it physically is is the whole
  point of recording one. A drive with no reading sorts *last*, never first — an absent temperature
  is not 0 °C, and treating it as such would put the least informative drive at the top.
- **Replace first** (`FEATURES.md` A24): a shortlist above the grid, ordered by how strongly each
  drive argues for replacement. It answers "which one do I buy this month?", which is a different
  question from the sort row's "how does each drive look today" — and it says so in its own header
  ("by urgency, not by verdict"). A numbered list, because the order *is* the answer. Each entry
  carries its band and the reasons that produced it, in the drive's own terms: a reader can disagree
  with a band and act on it, and cannot do either with an unexplained number. It is absent when
  nothing is worth replacing, and capped at five — past a handful, "which one first?" stops being an
  answer. A shortlist, not a list.
- **The header is three rows at most**, and each row has one job: the identity and the counts you
  filter by, then the controls that arrange the list, then one muted line for the notes the counts
  raise. The view switch and the exports are separated by a rule, because "how is this shown" and
  "what happens to this list" are different questions — an export is a file to keep, and shoulder to
  shoulder with the view switch it read as one more way to look at the page. The switch itself is a
  segmented pair rather than two loose buttons, since it is a mutually exclusive choice and two
  identical-looking buttons make the reader check which one is pressed. The standing instructions
  about rescanning and forgetting are gone: a line of help on every page load is read once and then
  only takes up room, and both actions are labelled on the things they act on.
  The sort field and its direction are one labelled pair, and the direction says **Ascending** or
  **Descending** with a real arrow: a bare ↑ in a ghost button beside a dropdown read as decoration
  rather than as the other half of the sort, which is exactly how it was reported. The transport filter
  carries its own label, and lists only the transports present on this host — in a fixed order rather
  than alphabetically, so the control keeps its shape as drives come and go.
- **Two views** (`FEATURES.md` D8): Cards and Table, switched from the controls row. The card grid
  is right for four drives and wrong for twenty-four, so the table gives one row per drive with
  bay, verdict, score, transport, model, capacity, temperature, age and last scan. Its headers
  sort: clicking the sorted column reverses it, and clicking another starts from that field's own
  idea of "first". Only sortable headers carry a `title`, because `th[title]` is what the stylesheet
  hangs the pointer cursor on — a header that looks clickable and is not is worse than a plain
  label. The table *replaces* the grid rather than nesting inside it, since a table in a grid cell
  is laid out as a single column and loses the columns it exists for. Both views are built from the
  same payloads and the same badge functions, so they cannot disagree about a drive's state. The
  choice is **stored rather than remembered by the tab**: it is a display preference like the theme
  and the temperature unit, so it lives in the settings table, is read before the first render, and
  travels in the config export. A reader who wants the table wants the table after a refresh, not cards
  again. The filter and the sort beside it stay in the module, because those are about the question
  being asked rather than about how this person likes to read.
- **Grouping** (`FEATURES.md` D9): a "Group by tag" toggle, offered only when at least one drive is
  tagged — a single "Untagged" heading is the list with a heading on it, not a grouping. It works in
  both views: cards get a heading and their own grid per group, and the table gets a separator row
  spanning every column so its header stays the only one. A drive with more than one tag appears
  under **each** of them, and the header says so outright, because a reader who notices the counts
  do not add up will otherwise conclude the app is wrong. Untagged drives come last.
- **Bay:** a drive with a recorded physical location leads its card with it. A serial identifies a
  drive; a bay *finds* it, and the person asking that question is standing in front of a machine
  rather than reading a screen carefully. Unset bays get no row at all — a gap in the bookkeeping
  is not a finding about the drive, and "Bay —" would read as a missing measurement (`FEATURES.md`
  D10).
- **Export** (`FEATURES.md` A23): one **Export…** button at the far end of the controls row, past a
  rule, opening a dialog with the **format** and the **scope**. It was two links, and two links at the
  end of a controls row read as two more ways to look at the page rather than as a file to keep — which
  is the one thing an export is. The dialog is also where each format gets to say what it is *for*,
  beside the choice it belongs to: the CSV is the inventory you sort when planning a purchase, the
  printable page is the sheet you attach when several drives go back at once. Scope is a tag rather
  than the dashboard's filter, deliberately — a filter is about what you are *looking at*, and an
  export that silently honoured it would hand you a file missing rows you never chose to exclude. The
  download is still an anchor with `download`, clicked and removed, because these are files to keep and
  the browser's own save dialog is the right one. Offered only when there is a fleet: with one drive
  the export is the single-drive report with fewer columns, and that report is better for the one thing
  a lone drive needs.
- **Schedule action** (`FEATURES.md` B13): the schedule editor's **Action** field chooses between
  "Scan the drives" and "Send a digest of the period", and the list has an **Action** column so a
  digest schedule is not mistaken for a scan. The two axes are independent — the timing controls below
  answer *when*, the action answers *what* — which is why a weekly digest is a cron schedule with
  `action: digest` rather than a second kind of schedule.
- **Prometheus metrics** (`FEATURES.md` C6): a **Prometheus metrics** checkbox in Alerts and
  retention, off by default, which serves `GET /metrics` in the text exposition format. The endpoint
  costs nothing by itself; turning it on invites another service to poll this app, and that is the
  operator's decision rather than a default — the same reasoning as a self-test being opt-in.
- **Alert acknowledgement** (`FEATURES.md` B7): a **Notes**-row **Acknowledge** control on a drive's
  page, with a note saying why. It silences the alerts for that condition and leaves the verdict
  alone — the drive still reads as caution, because it is caution — and it is **spent the moment the
  verdict moves**, so it can never hide a drive getting worse. The verdict card says outright that the
  condition is acknowledged, with the note and when; without that the reader would conclude the app
  had lost the alert. A drive that vanishes and a self-test you asked for still alert: an
  acknowledgement covers a condition, not a drive.
- **Action buttons on notifications** (`FEATURES.md` B10): ntfy notifications carry an "Open drive" /
  "Open dashboard" button, from a new **Public URL** setting. Empty means no button — the app cannot
  know its own external address, and a wrong link is worse than none.
- **Self-test** (`FEATURES.md` A19): a short and a long button, each behind a confirmation that says
  what the test does and what it costs, with the drive's own estimate of the time. A running test
  shows its own percentage remaining and a **Stop test** button. The card also carries the **last
  finished result**: "did the test I ran pass?" is asked here, so the answer belongs here rather than
  only in the log table above, which is drawn from the last scan. A test this card started that is
  over before the first reading says so, and triggers the one rescan that puts its outcome into the
  stored log — without that a short test left no trace anywhere, because a fifteen-second poll only
  ever sees the drive after it finished. A drive whose self-test log this app cannot read is told so
  rather than shown an empty panel: an empty panel and a test that never ran look identical, and only
  one of them is true.
  A **long** test the drive refuses disables its own button and says so in the card, because a button
  inviting a second attempt at something the drive has already declined is worse than no button. It is
  disabled for the life of the card rather than remembered: "refused once" is not the same fact as
  "cannot" — a drive can refuse a command it would take a minute later — and nothing smartctl reports
  in advance says which tests a drive supports, so greying it out on a guess would hide a capability
  that works. The short test stays available, because a refused extended test says nothing about it.
- **Heating together** (`FEATURES.md` A25): a card that appears only when a group is actually moving,
  the same rule as the other two. One block per group, each naming the group, the ratio (`3 of 4
  drives`), the shared rise and the hottest current reading, with the drives as links to their pages.
  The ratio is shown rather than a bare count because six of six is a different finding from six of
  twenty-four, and that judgement belongs to the reader. The card is headed **"a correlation, not a
  diagnosis"**, and its wording deliberately names what moved and where to look and never what is
  wrong — a RAID rebuild heats everything too, and the reader knows whether one is running. It is
  ordered first of the three panels: a condition comes before a suggestion.
- **Testing session** (`FEATURES.md` D11): a card that appears only when a drive is being tested, the
  same rule as the replace-first shortlist — a panel that is always present stops being read. It names
  every drive in the batch as an inline chip (the session *is* the batch, so a shortlist of it would be
  answering a different question, but eight rows of list for eight drives is a lot of card for a set of
  names) and it says what the state *means* rather than what the tag is called: "Drives being tested
  rather than used. They are left out of the health counts above: a drive that fails while you are
  testing it is a result, not an alarm." The tag name is machinery, and a reader who wants to know how a
  drive got here will find it in that drive's own tags. The card carries the batch's three verbs:
  **Test all** (short self-tests, sequential, confirmed first because a self-test is the only thing this
  app does that writes to a drive), **Export batch** (the fleet CSV scoped to the tag, so the batch gets
  its own sheet), and **Forget all** (one confirmation listing the drives, then a client-side loop,
  because a request that deletes every drive matching a tag is one mis-click from clearing the fleet).
  Partial failures are named per drive — "2 of 6 failed" tells you nothing about which two to go and
  look at.
- **Getting a drive onto the bench is a checkbox, not a tag you have to know to type.** The device's
  **Notes** dialog carries **Being tested — left out of the fleet counts**, and it and the tags field
  are kept in step both ways. That is where the feature becomes discoverable: it is the dialog that
  already describes the drive, so it is where somebody would look, and it states the consequence rather
  than the mechanism. It is deliberately *not* on the dashboard — a card explaining a tag you have never
  used is a card explaining nothing.
- **Bench drives and the counts:** they are left out of the health summary, and the head says so
  ("6 are tagged bench and left out of those counts"), because a reader who adds the counts up and
  gets a number smaller than the list is owed the reason. They are **not** hidden: they stay in the
  list, in the search and in every export, and a **Bench only (6)** control filters the list to them
  without turning on grouping.
- **Disconnected:** a drive absent from the latest scan keeps its record — its notes
  and history must survive an unplug — but is flagged `is_missing`. Those cards are
  hidden by default; a **Show N disconnected** toggle reveals them, dimmed and
  carrying a *Not connected* badge instead of a verdict. They are excluded from the
  summary-bar counts, so a vanished drive can never silently flatter the health
  totals.
- **No S.M.A.R.T.:** a drive that is present but cannot report S.M.A.R.T. at all
  carries a *No S.M.A.R.T.* badge, not *Unknown* — "unknown" would read as "we have
  not worked it out yet", when the truth is that most USB flash drives and card
  readers simply do not implement it. Its health ring is omitted rather than drawn
  permanently empty, since there is no score to draw.
- **Blocked:** a drive the container was *not allowed* to read carries a *Blocked* badge
  instead, because that is the container's problem rather than the drive's and the two
  need different fixes. The distinction is made server-side (`smart_cause`), since
  smartctl reports both as the same "Operation not permitted".
- **Worsening:** a drive whose bad-sector count is rising over the recent window carries
  a *Worsening* badge alongside its verdict. Separate from the verdict because the two
  answer different questions — the verdict says how the drive looks *now*, this says which
  way it is going, and a drive can be green today and still be the one worth replacing
  first. Reported as a direction, never as a date (`SCOPE.md` §2.2).

### 7.2 Device detail
- **Header:** editable inline name, model, serial, firmware, capacity, drive age.
- **Health block:** large gauge + verdict badge + plain-language rationale
  ("5 pending sectors").
- **Since the last scan** (`FEATURES.md` A18): the first card in the grid, because "what changed" is
  the news and the cards below it are reference material. One `was → now` line per field that moved,
  with the field name fixed-width so the arrows line up and the list reads as a column. An absent
  end is an em dash rather than a zero — a reading the drive never gave is not a reading of zero.
  "Nothing changed" is stated in words, because an empty panel would read as "we did not look". The
  counters that moved *and* are a finding get a table; the routine ones are counted and said out
  loud ("4 other counters moved, which is ordinary wear") rather than dropped.
- **Trend card:** per-metric sparklines with the current value and the change across the series,
  over a 7/30/90-day/all range selector. The x-axis is **time rather than reading index**
  (`ADR-023`): a fixed range is a *calendar* window, so 30 days means the last 30 days whether or
  not anything was recorded in them, and a drive that stopped being scanned leaves a visible gap
  instead of a line that quietly ends at the right edge. A series that began five days ago occupies
  its own fifth of the window rather than being stretched edge to edge. One shared date axis sits
  under the card rather than one per sparkline — four charts in one window need one axis, and four
  would be noise — so the span is readable without hovering. "All" has no calendar to anchor to, so
  it spans exactly the history that exists. The change figure is measured across the *series*, not
  the window: a drive watched for five days has five days of change, and reporting 30 would invent
  a trend. Per-metric y-scaling stays automatic.
- **Two columns**, from a single `.panel-grid .panel-grid-pair`: **Identity** beside
  **Usage and media**, then the trend card beside the attribute table, with the notes card
  spanning both. One grid for the whole page, not one per row — two separate grids each
  size their own columns and their gutters drift apart as soon as their contents differ.
  The grid is capped at two columns (§6): at 2560px an auto-fitting grid fits four panels
  across, which is not the layout this page was designed around.
- **Even gutters.** Cards are stretched to their row, deliberately *not* `align-items:
  start`. A grid row is as tall as its tallest card either way, so a short card that
  stops early leaves the gap *beneath* it looking wider than the gap *beside* it;
  stretching moves that slack inside the card, where it reads as padding.
- Each card holds its own heading and content in a `.card.stack`, so the internal
  spacing comes from the container's gap and never from a per-heading margin.
- **Identity card:** model, model family, serial, firmware, capacity, interface
  speed, transport, rotation, removable, WWN, device path.
- **Usage and media card:** power-on hours, power cycles, temperature, total data
  read / written, life remaining, mountpoints, filesystem, first seen, last scanned.
  Throughput and power figures come from the API's derived `metrics` block
  (`API.md`), so ATA and NVMe drives read identically.
- **Outlook** (`.projection`, at the foot of the Usage card): the longevity estimate from
  the drive's history — a sentence, not a figure. Deliberately kept out of the stat grid
  above it: a number in that column reads as a measurement, and this is an extrapolation
  from a handful of readings. The confidence (`low` / `medium`) and the caveat are shown
  inline rather than behind a tooltip, because a caveat nobody reads is not a caveat. A
  *Worsening* badge appears here too when bad sectors are rising. A drive with too little
  history says "Not enough history yet" instead of guessing.
- **Mountpoints / Filesystem:** taken from the disk's *partitions* — a whole-disk
  node is never mounted itself (`DATA_MODEL.md`). Filesystem shows every distinct
  type found, e.g. `ext4, vfat`, followed by the filesystem version when lsblk
  reports one (`exfat 1.0`).
- **Drives with no S.M.A.R.T. reading:** the page shows a **notice** — a neutral panel,
  not an error box — and it distinguishes two situations that smartctl reports
  identically (`smart_cause`, `API.md`):
  - *A drive that cannot report S.M.A.R.T.* says so, explains that most USB flash drives
    and card readers do not implement it, notes that the identity, capacity and filesystem
    shown come from the operating system rather than the drive, and offers the one thing
    that sometimes fixes it: a per-device `smartctl -d` transport override with
    **Apply & rescan**, shown as currently set.
  - *A read the container was not allowed to make* says **that** instead, names which of
    the three gates is closed, and prints the exact compose fragment to add. Telling a
    container user their drive "does not report S.M.A.R.T." when it was simply blocked
    sends them looking at the hardware instead of the compose file — which is the whole
    reason the cause is classified server-side rather than left as raw text.

  The dashboard card carries a *Blocked* badge for the second case and *No S.M.A.R.T.*
  for the first, so the two are distinguishable without opening the drive.
- **Trend hover readout:** hovering a sparkline shows a vertical marker at the nearest
  reading plus a readout of its value, date and time — stacked on three lines rather
  than run together, because one line wide enough to hold a full timestamp spans most of
  the card. The ends are anchored rather than centred so the readout cannot run off the
  card. Arrow keys step through the readings and `Esc`/blur dismisses it, so the figures
  are reachable without a mouse.
- **Attribute table** (§6) — for NVMe, the full health log: critical warning,
  spare, percentage used, data units read/written (count **and** humanised size),
  host read/write commands, controller busy time, power cycles/hours, unsafe
  shutdowns, media errors, error-log entries, warning/critical temperature time.
  Sixteen counters in a flat list read as a wall, so the NVMe log is **grouped** —
  *Health*, *Lifetime*, *Workload*, *Errors* — and set a size up from the attribute
  table, which needs `--text-xs` for its seven columns and this does not.
  A "show raw `smartctl` output" toggle sits alongside it. It shows the output stored
  for the drive from the last scan — including when the read *failed*, where smartctl's
  own complaint is the most useful thing on the page — so it is a reliable way to see
  whether a drive is unsupported, mis-addressed, or blocked by container capabilities
  (`DEPLOYMENT.md`).
- **Attribute help** (`FEATURES.md` A12) — a `?` beside each interpreted attribute and each
  NVMe counter, opening a short dialog: what the field is, and how to read it. The second
  half is the part that earns its place, because it is what stops attribute 199 (usually a
  SATA cable) or attribute 1 (not comparable between vendors) from reading as a drive in
  trouble. A row the drive *itself* reports at or below its own threshold is tinted and
  badged *At threshold* — the drive's own alarm, never a threshold this app invented.
- **Actions:** scan this device, export report (formats from settings), edit
  notes/tags, per-device transport override, hide.
- **Notes editor:** free-form notes + tags; optimistic save with rollback.

### 7.3 Schedules
- List of schedules: label, kind (interval/cron), next run, last run, enable toggle,
  "run now", edit, delete.
- Editor dialog: interval picker **or** cron expression with a plain-English preview
  ("every day at 03:00").

### 7.4 Notifications
- Channel list with type icon, label, enabled toggle, "send test".
- The card's subscription line says what the channel *receives*, not what is ticked. An empty
  subscription is every event — the setting lists what has been narrowed away, so nothing listed
  means nothing excluded — and the card reads **Everything** with the two exceptions named (the
  scheduled-scan heartbeat and the group-heat finding) instead of "No events subscribed". That case
  is not rare: it is exactly the channel a digest is set up on, and the digest arrives *because*
  nothing was narrowed away.
- Channel editor: type-specific fields (ntfy server/topic/token; webhook URL/method/
  headers); event-subscription checkboxes.
- **Test** in the editor footer sends one event through the settings *as they stand on
  screen*, saved or not, via `POST /api/channels/test` (`API.md`). It deliberately does
  not save and does not close the dialog: the point is to fix a misconfigured token or
  URL and press Test again, in place. Testing a draft writes nothing — including no
  delivery-log row, since there is no stored channel to attribute one to — so **Save**
  remains the only thing that persists anything.
- Recent-deliveries log (sent / failed / squelched).
- **Even spacing.** The channel grid, the deliveries log and the maintenance-window card
  are siblings in the page column and are spaced by that column's gap alone. No card
  carries a vertical margin of its own, and paragraphs carry none either (`p` is reset
  in the stylesheet) — a browser-default paragraph margin on top of the flex gap is
  exactly what made cards containing a `<p>` look differently spaced from those that
  did not.

### 7.5 Settings
- Grouped sections: **Health thresholds**, **Alerts and retention**, **Appearance**, and
  **Import and export** (`FEATURES.md` C7). The last is two controls: an **Export** anchor with
  `download` — it is a file to keep, so the browser's own save dialog is the right one — and an
  **Import** button that opens a file picker. The import is confirmed *before* the picker rather
  than after: the user should know what the file will do before they spend time finding it, and the
  one destructive part (a file carrying schedules *replaces* the list) is the thing they most need
  to know. A malformed file reports what went wrong rather than surfacing `Unexpected token <`,
  which is the common failure and explains nothing.
- **One sheet with sections, in a single column.** Four cards of different heights gave a ragged bottom,
  and reaching the shortest one meant scrolling past the tallest — so the grid did not make the page
  shorter, it made it uneven. The first attempt at a fix laid each section's rows out in *two* columns,
  and that was worse: a row is a label, an explanation and a control, so one line of the page carried six
  columns of text and the eye had nowhere to go. Rows are one per line now — label and explanation on the
  left, control right-aligned so every value lands in the same column — and the height is bought back
  where it belongs: by keeping the explanations to one or two lines instead of four, which is most of
  what made the page tall in the first place.
- Numeric settings are a **list of rows**, not a grid of labelled boxes: the label and
  its explanation sit together on the left and the control is right-aligned, so every
  value lands in one column and the page can be scanned and compared at a glance. Each
  numeric row pairs a **slider** with a number box — drag to explore, type for
  precision — and the two stay in sync.
- **Temperature unit** (°C / °F) is a display preference. Celsius is canonical in the
  API, the database, snapshots and notification payloads, because that is what drives
  report; only rendering converts. The temperature *threshold* is stored in Celsius
  too, so its slider is labelled, bounded and converted in the chosen unit — otherwise
  the same threshold would read as two different temperatures in two places.
  Switching the unit re-renders the row, carrying unsaved values across in Celsius so
  nothing typed is silently reinterpreted.
- Alert text follows the same unit, so a notification and the UI never disagree.
- There is deliberately **no About card**, and no version in the sidebar footer: the
  version appears once, in the brand mark at the top of the sidebar, and repeating it
  below **Scan now** was noise. The footer carries the two facts that are not already
  on screen — the freshness note ("Data reflects the last scan.") and the provenance
  note ("S.M.A.R.T. via smartmontools — the app never writes to a drive.").
- The version shown is stamped at build time (`-X main.version`), so it identifies the
  revision a container is actually running rather than a constant. An unstamped build
  says `0.0.0-dev`, which is the honest answer.
- **The knowledge base card** (`FEATURES.md` A21) is the fourth card, and it spans the grid
  rather than taking a cell: it carries a JSON editor, and a text box squeezed into a 460px
  column is a text box nobody will use. Its status line is the honest part — how many families
  are known, how many shipped with this build, and how old the fetched copy is — because the
  refresh is a *button* and a reader cannot decide whether it is worth pressing without seeing
  the age. The editor is a text area rather than a form, deliberately: the format *is* the
  interface, the same JSON the bundled file and the refresh URL use, so there is one thing to
  learn rather than a form that can only express what the form thought of. The parse error comes
  back verbatim and is shown **beside the box**, in a `.knowledge-error` block rather than a
  toast — these messages name a URL and a JSON field, and a toast disappears before anyone can
  read it. A copy fetched from a URL that has since changed is called out, because its age is
  not evidence about the URL the user is looking at. **Download the shipped table** is an anchor
  with a `data:` href and `download`, which is how somebody starts their own copy to point the
  refresh URL at.

## 8. Interaction & feedback

- **Optimistic UI** for all mutations (notes, schedules, settings) with rollback +
  error toast on failure.
- **Scan progress** is SSE-driven (`GET /api/events`); the UI never polls.
- **Keyboard shortcuts** (desktop): `s` = scan, `/` = focus filter, `Esc` = close
  dialog, `?` = shortcut help.
- **Reduced motion** honoured via `prefers-reduced-motion`.
- **No layout shift** during scans — cards reserve their space (skeletons match final
  dimensions).
- **A re-render never moves the reader.** A refresh, a rescan or a scan finishing re-draws the page it
  is already on, so the scroll position has to survive it. Every view empties its container and *then*
  awaits its data, and a page that is briefly short makes the browser clamp the scroll position to the
  top — which is how "Refresh now" threw you out of the card you were reading. The container is held at
  its old height across the swap and released afterwards, so the position stays valid and only a
  genuinely shorter page moves the reader. Navigation is the opposite case and starts at the top,
  because a page you have just asked for is one you have not read yet.

## 9. Accessibility

- WCAG **AA** contrast in every theme, including custom accents (auto-derived
  contrast, §5).
- Colour is never the sole carrier of meaning — every verdict has an icon **and** a
  word.
- Semantic HTML + ARIA: `nav`, `table`, `dialog`, and `aria-live` regions for scan
  progress and toasts.
- Visible focus rings everywhere; focus trapped inside open dialogs; full keyboard
  operability.
- `prefers-reduced-motion` and `prefers-color-scheme` respected.

## 10. Implementation approach (why this avoids prior agentic-UI pain)

- **Static HTML shell + vanilla ES modules.** No framework, no build step, no npm
  dependency tree, no framework version churn. The biggest source of "the UI broke
  again" in iterative development is removed by construction.
- **One CSS file of tokens + one of components.** Components are small functions that
  return DOM nodes; no virtual DOM to reason about.
- **Theming touches only tokens** (§5) — adding or changing a theme never edits
  component code.
- **Server-rendered theme attribute** eliminates the flash-of-wrong-theme class of
  bugs.
- **The component inventory (§6) and screen specs (§7) are the contract** — work is
  "build component X to spec", not "make it look nice", which is what keeps iterative
  UI work from drifting.

## 11. UI acceptance criteria (definition of done)

A UI change is not done until:

1. Every affected screen has **empty, loading, scanning, error, and populated**
   states implemented.
2. It renders correctly in **light, dark, and a custom accent** theme, with AA
   contrast.
3. It is usable at **360px width** and at desktop width.
4. It works with **keyboard only**, with visible focus.
5. It makes **no external network requests** (no CDN, no web fonts).
6. No hard-coded colours/radii/spacing — tokens only (reviewable by grepping the CSS
   for `#` and `px` values outside the token block).
7. Scan-related UI updates arrive via **SSE**, never polling.
8. Lighthouse performance ≥ 95 on the dashboard.


