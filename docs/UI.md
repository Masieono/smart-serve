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
4. **Honest freshness.** Every screen shows "last scanned …", and the UI never implies
   live monitoring — it reflects the last scan (`ADR-007`). The one exception is stated
   rather than silent: opening a drive page whose data is past `device_refresh_minutes`
   reads *that drive* and says "reading the drive now…" while it does (`FEATURES.md` A28).
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
views are bookmarkable and the browser back button works. A navigation starts the new
page at the top — the wordmark and the sidebar entries all go through it — while a
refresh, a rescan or a scan finishing keeps your place (§8).

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
| **Fleet table** | One row per drive: Drive (name, then model · transport · capacity · first mountpoint), Health, Usage, Temp, Powered on. The health cell is the verdict dot, the word and the 0–100 score together, because they are the same fact. A row needing attention carries a 2px inset rule in its state colour down the left edge. Rows sort worst first (bad, caution, unknown, good), ties by name. |
| **Glance line** | One line under every page title: counts, then the thing a reader wants next (when the next run is, when the last delivery was). On the fleet the counts are filters; clicking one narrows the table to that state and clicking it again clears. |
| **Section heading pattern** | Every screen and every section is a title row: `h2` on the left, an aside on the right (a count, a control, or one sentence). A rule appears only *between* sections; rows inside a section are separated by space, never by a line per row. |
| **Save bar** | Settings only. Pinned to the bottom of the window and shown only while something is dirty; carries the state text, Revert, and Save. Opaque, so a grid behind it cannot read through the buttons. |
| **Theme swatch grid** | Nineteen chips, each carrying its own `data-theme` so the palette's real `--palette-*` values resolve inside it. Never reads the semantic tokens: those are computed at `:root` and inherit as colours, so a descendant cannot re-resolve them. |
| **Weekday strip** | Seven cells for the schedule editor's day picker, each a label wrapping a visually hidden checkbox, selected state via `:has(input:checked)`. Guarded by `@supports selector(:has(*))`; a browser without `:has` gets the plain checkbox row. |
| **Attribute table** | Zebra rows; ID / name / value / worst / threshold / interpreted raw. Every row the interpreter acts on carries a `?` that opens an explanation of what the field is and how to read it, and a row is tinted and badged *At threshold* when the drive reports it at or below its own threshold (`FEATURES.md` A12). Not sortable — the table is short enough to read, and sorting it is not worth a control. |
| **Sparkline / trend card** | Inline SVG per metric (health score, temperature, power-on hours, wear) with the current value and the change across the window, plus a 7 / 30 / 90 day / all range selector. Hovering (or arrowing, when focused) shows a marker and a three-line readout — value, date, time — for that reading. A row with fewer than two readings is dropped — one point is not a trend. |
| **Notice** | A neutral panel for an explanation that is not a fault. Two of them: "this drive does not report S.M.A.R.T." (with the transport-override control), and — added later — "the container was not allowed to read this drive", which names which of the three access gates is closed and prints the compose fragment that opens it. Deliberately not an error box in either case: neither is the drive failing. |
| **Export dialog** | One row per format (plain text / web page / spreadsheet / JSON) with what each is for, rather than a single button that silently picks one. |
| **Scan button** | Primary action. States: idle · scanning (spinner + "3/5 devices") · done (toast). |
| **Toasts** | Scan complete, saved, notification sent; auto-dismiss, stacked. |
| **Dialogs / sheets** | Schedule editor, channel editor, maintenance window, device notes, acknowledge, export, confirmations. Full-screen sheets on mobile. A dialog holding unsaved input — every editor above — does **not** close on a click outside it; only **Cancel** and `Esc` do, because a stray click beside a half-filled form should cost the click rather than the form. Confirmations and the export picker still dismiss on a backdrop click, where there is nothing to lose. Dismissal turns on where the *press* landed, not where the release did: a drag that starts on a field and ends outside the box is a text selection, not a dismissal. |
| **Empty states** | "No scan yet — press Scan to discover drives", "No devices found", "No schedules", "No history yet — trends appear once this drive has been scanned a few times". |
| **Skeletons** | Placeholder shapes while data loads — never a blank screen. |
| **Settings forms** | Inline validation; each field shows its **effective value, default, and source** (`env` or `db`, per `ADR-009`). |

## 7. Screen specifications

### 7.1 Fleet (`/`)

The screen answers one question: **is anything wrong here?** Everything on it serves that, and
everything that did not (a score ring per drive, a shortlist repeating the top three rows) has been
cut.

- **Title, then the glance line, then the table.** No hero, no summary block. The counts on the
  glance line (`0 failing`, `1 caution`, `5 healthy`, `2 no reading`) are filters: clicking one
  narrows the table, clicking it again clears. A count that cannot be clicked is the same information
  with a dead affordance attached.
- **Columns:** Drive · Health · Usage · Temp · Powered on. Health carries verdict word and score in one
  cell. Usage is a thin bar plus its percentage, and reads `n/a` when the host exposes no filesystem.
  Temperature and powered-on hours are right-aligned so a column of numbers scans vertically.
- **Order** is severity first: bad, caution, unknown (which includes drives the app could not read),
  good. Ties break on name so a rack of identical drives does not reshuffle between renders. A drive
  with no reading sorts *last*: an absent temperature is not 0 °C.
- **The attention edge.** A row whose drive is caution or failing gets a 2px inset rule in its state
  colour down the left edge, and "no reading" keeps one line of the drive's own reason underneath.
- **Actions:** `Scan now` in the page header, and the sidebar's global scan. Nothing else; opening a
  drive is the row itself.
- **States:** skeleton rows while loading; one card per drive updating live over SSE during a scan (no
  polling, `ADR-006`); an onboarding empty state with a prominent Scan when no scan has run.
- **Below the table:** `N drives`, and links to Schedules and Settings.

There is no card view and no replace-first shortlist. Both were part of the previous design and were
retired once the table carried the same information in less space (`ADR-032`).

### 7.2 Device detail (`/device/<serial>`)

Sections run **read-only first, and the one thing that writes to a drive last**. Within the read-only
part the order is by who asks: facts and storage for everyone, trends for anyone watching a drive age,
attributes for the power user.

1. **Identity and verdict.** Drive name (user name, else model, else serial), then model · transport ·
   firmware and the serial, then the verdict: dot, word, `score N of 100`, and the drive's own reason
   when it has one. Actions: `Scan this drive`, `Export`.
2. **Freshness line.** `data from 3d ago`, or `re-reading this drive` while it reads. Opening a drive
   re-reads it when the stored data is older than `device_refresh_minutes` (default 15; `0` never),
   because a read is the one thing here that can wake a parked disk.
3. **Facts** — capacity, mountpoint, powered on, wear, device node, size, temperature, last scanned.
   Values the drive did not report read `n/a`, never `0`.
4. **Disk space** — `43% used of 1.8 TB on /mnt/media`, then the disk map, then the partition table.
   The map and the table mark each other both ways: pointing at a segment marks its row and vice
   versa. The map *is* the usage bar when a layout exists; drawing both states the same figure twice.
5. **Trends** — six cards in two rows of three: health score, temperature, power-on hours, wear,
   storage, bad sectors. Every reading draws a dot, so a gap in scanning is visible rather than
   implied. The range picker (7d / 30d / 90d / All) redraws only the trend host, never the page, and
   `All` is the default: a young install in a 30-day window draws a short line pinned to the right
   edge, which reads as a broken chart. Hovering or arrowing shows value, date and time.
6. **Attributes** — ID, name, value, worst, threshold, interpreted raw. A `?` on every attribute the
   interpreter acts on explains what it is. A row is marked only when the drive reports its **own**
   attribute at or below its **own** threshold; nothing is marked because the app could not read it.
   The aside reads `N reported · M at threshold`.
7. **NVMe health log** — NVMe drives only, as a grid of labelled values rather than a two-column
   table that would put labels at one edge and values at the other.
8. **Self-test** — the only control in the app that writes to a drive (`FEATURES.md` A19). Short and
   long test, progress while it runs, abort, and the drive's own self-test log underneath. This is the
   one place the page reads a drive live, and only while a test it started is still running.
9. **Footer** — provenance (`Read from /dev/sda 3d ago`, or `Never scanned`) and the export entry
   point.

### 7.3 Schedules
- List of schedules: label, action, kind (interval/cron), next run, last run, enable toggle,
  "run now", edit, delete.
- Editor dialog: interval picker **or** cron expression with a plain-English preview
  ("every day at 03:00"), and an **Action** of *Scan the drives*, *Send a digest of the period* or
  *Run a self-test on each drive*. The self-test action adds a **Test** picker (short or long) and a
  hint naming the two things that make it different: it runs one drive at a time, and it is the only
  schedule that writes to the drives.
- While a self-test pass is draining, a **notice** above the table says how many drives are done and
  which one is testing now. A pass runs for hours, so a `last run` that has already passed is not
  enough for a reader to know what is happening. See `ADR-024`.

### 7.4 Notifications
- Channel list with type icon, label, enabled toggle, "send test".
- The card's subscription line says what the channel *receives*, not what is ticked. An empty
  subscription is every event — the setting lists what has been narrowed away, so nothing listed
  means nothing excluded — and the card reads **Everything** with the two exceptions named (the
  scheduled-scan heartbeat and the group-heat finding) instead of "No events subscribed". That case
  is not rare: it is exactly the channel a digest is set up on, and the digest arrives *because*
  nothing was narrowed away.
- The card carries a **second** line for the group scope (`FEATURES.md` B12), read the same way: a
  scoped channel says **Only drives tagged** and names them, and an unscoped one says **Every group**
  rather than nothing at all — silence there would read as a channel about no drives, which is the
  opposite of the truth.
- Channel editor: type-specific fields (ntfy server/topic/token; webhook URL/method/
  headers); event-subscription checkboxes; and a **group scope** — one checkbox per tag in use, with
  the same "nothing ticked means everything" default as the events above it. Offered only when the
  fleet actually has tags; otherwise the field explains how to create one, because an empty list of
  groups to choose from answers nothing.
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
- Grouped sections: **Health thresholds**, **Per-model thresholds**, **Alerts and retention**,
  **Appearance**, and **Import and export** (`FEATURES.md` C7). The last is two controls: an
  **Export** anchor with
  `download` — it is a file to keep, so the browser's own save dialog is the right one — and an
  **Import** button that opens a file picker. The import is confirmed *before* the picker rather
  than after: the user should know what the file will do before they spend time finding it, and the
  one destructive part (a file carrying schedules *replaces* the list) is the thing they most need
  to know. A malformed file reports what went wrong rather than surfacing `Unexpected token <`,
  which is the common failure and explains nothing.
- **Per-model thresholds are a table, not a schema** (`FEATURES.md` A3). One row per model in the
  fleet, one column per threshold, and an empty cell means "same as above" — the value it would
  inherit is shown greyed as the placeholder, so the table reads as the effective configuration
  rather than as a list of exceptions. A row whose model has no overrides says so; a row that has
  any is marked *overridden* and carries a **Reset** that drops that model's entry. A model kept in
  the stored JSON but no longer in the fleet still gets a row, because an override for an unplugged
  drive is still the user's. **Advanced (JSON)** holds the same data for keys a column does not
  cover, and it is the value that is saved: the table edits it rather than keeping a second copy.
  **Load the fleet's models** merges an entry for every model at the values currently on screen,
  never replacing what is already written, and the server refuses a key it does not know, reported
  beside the table as well as in a toast.
- **One sheet with sections, in a single column.** Four cards of different heights gave a ragged bottom,
  and reaching the shortest one meant scrolling past the tallest — so the grid did not make the page
  shorter, it made it uneven. The first attempt at a fix laid each section's rows out in *two* columns,
  and that was worse: a row is a label, an explanation and a control, so one line of the page carried six
  columns of text and the eye had nowhere to go. Rows are one per line now — label and explanation on the
  left, control right-aligned so every value lands in the same column — and the height is bought back
  where it belongs: by keeping the explanations to one or two lines instead of four, which is most of
  what made the page tall in the first place.
- **Settings is four pages, not one.** Thresholds, Notifications, Appearance and Data, each reached
  from the sidebar — the sections are listed under the Settings entry while you are in that part of
  the app, and only then. The first attempt kept them as collapsible headings on one screen, which
  made the wall shorter to look at without making it shorter to use; a dozen controls split four ways
  is four short pages, and nothing needs folding up. `/settings` itself renders the first page, so the
  plain sidebar entry and an old bookmark both land somewhere.
- **Save is per page.** Each page sends only its own fields, and `SetSettings` merges per key, so
  saving Appearance cannot disturb a threshold — which is what makes per-page Save safe rather than a
  way to silently drop somebody's other edits. **Save is disabled until something changes**, and
  leaving a page with unsaved edits asks first: nothing here writes to a drive, so the prompt is not a
  safety net, it is the difference between discarding a change deliberately and losing a number you
  typed. The guard is registered by the view and cleared on every route, so it cannot outlive the page
  that set it.
- **A page loads only what it draws.** The per-model editor wants the fleet to seed from and the
  knowledge card wants the knowledge status; Appearance and Notifications ask for neither.
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

### 7.6 Side-by-side comparison (`FEATURES.md` D6)
- The dense fleet table carries an **optional select box column** — only that view, and only when
  the caller asks for it. Tick two to six drives and a **Compare N** button appears in the toolbar
  beside the export controls; the selection survives opening a drive and coming back, because that is
  exactly when somebody is picking a second drive.
- The comparison opens in a **dialog**, not a route: it is a look rather than a place, and a URL for
  it is one people bookmark into a 404 after replacing a drive. Columns are the drives in the order
  they were ticked.
- The table is aligned **by attribute id** and comes from the server (`GET /api/compare`). Rows are
  marked in two weights: a **flagged** cell is the drive's *own* threshold (loud), and a **differing**
  row is one the drives disagree about (quiet). The count of rows worth reading is stated above the
  table.
- A cell shows the normalised value, with the **raw count beneath it when the two differ** — a healthy
  drive reports `100` for reallocated sectors whatever the real count is, so the raw is the number
  that carries the finding. A drive that does not have an attribute shows an em dash, never a zero.
- The attribute column is sticky, so it stays put while the drive columns scroll sideways.

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


