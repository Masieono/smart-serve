# Smart-Serve: UI / UX Design

A clean, glanceable interface is a first-class requirement, not a finish line, so this document is
prescriptive.

> **Implementation status:** the interface is implemented under `internal/webui` (embedded assets
> plus a server-rendered theme attribute). There is one UI, and the five pages below are all of it.

## 1. Design principles

1. **Glanceable first.** Whether anything is wrong must be readable in two seconds from the fleet table.
   Verdict and score are the headline.
2. **Calm and clean.** Generous whitespace, restrained colour; colour carries *meaning* (health state) and
   is never decoration.
3. **CrystalDiskInfo's density, modernised.** The full attribute table for power users, a clear verdict
   for everyone else.
4. **Honest freshness.** Every screen says when the data was read, and the UI never implies live
   monitoring (`ADR-007`). The one exception is stated rather than silent: opening a drive whose data is
   past `device_refresh_minutes` reads *that drive* and says "reading the drive now…" while it does
   (`FEATURES.md` A28).
5. **No framework, no CDN, no external fonts.** System font stack, inline SVG icons, CSS custom
   properties: offline-capable and immune to framework churn.
6. **Every state is designed.** Empty, loading, scanning, error and populated states are specified below.
   A blank screen is a bug.

## 2. Information architecture and navigation

One shell, five pages (the fleet's nav entry reads **Devices**):

| Page | Route | Purpose |
|---|---|---|
| **Fleet** | `/` | One row per drive, severity first, plus a global scan |
| **Device** | `/device/<serial>` | Full report for one drive |
| **Schedules** | `/schedules` | Scheduled scans, digests and self-test passes |
| **Notifications** | `/notifications` | Channels, recent deliveries, maintenance windows |
| **Settings** | `/settings/<page>` | Thresholds, Notifications, Appearance, Data |

The sidebar is the only persistent shell element: a wordmark button (name and build version) that goes
home, the nav, a primary **Scan now** button, and two notes saying that the data reflects the last scan
and that smartmontools never writes to a drive. The Settings entry reveals its four sub-pages underneath
it, and only while you are in that section. A **Sign out** control appears below the nav only when the
optional sign-in layer is on (`ADR-033`). The wordmark is a `<button>` because every navigation goes
through `ctx.navigate`, which owns the base path (`ADR-015`).

Routing uses the History API, and `/settings` renders its first page so an old bookmark is never a dead
end. A navigation starts at the top; a re-render of the page already on screen keeps your place (§8).

## 3. Layout and responsive behaviour

- One column of sections per page, content padding `--space-5` (`--space-4` below 640). **No maximum
  content width:** tables and the device report use the full window, because a capped column wastes a
  wide screen. `--form-max` (760px) exists for controls that would look absurd stretched.
- **1024px:** above it the sidebar is sticky and full height and the topbar is hidden; at or below it the
  shell is one column, the sidebar an off-canvas drawer (`translateX(-100%)`, opened by the topbar toggle,
  closed on navigation), and the topbar holds only that toggle.
- **1100px and 760px** narrow the device page grids: trends go three columns, two, then one; the NVMe log
  grid two, then one. **Below 640px** padding tightens, setting rows stack their control under the label,
  and toasts span the window. Fixed-width tables scroll sideways rather than collapsing into unreadable
  cells.

## 4. Design tokens

All styling flows from tokens in `assets/styles/tokens.css`. Components never hard-code a colour, radius
or spacing value, which is what makes theming a token swap.

- **Spacing:** `--space-1` 4, `-2` 8, `-3` 12, `-4` 16, `-5` 24, `-6` 32, `-7` 48 px. **Layout:**
  `--sidebar-w` 232px, `--form-max` 760px.
- **Type:** `--text-xs` 12, `-sm` 14, `-md` 16, `-lg` 20, `-xl` 24, `-2xl` 32 px; `--font-sans` is the
  system stack, `--font-mono` for serials and attribute values.
- **Radii:** `--radius-sm` 6 (controls), `-md` 10 (cards), `-lg` 16 (dialogs), `-pill`. **Elevation:**
  `--shadow-1` (card), `--shadow-2` (dialog, drawer).
- **Palette:** `--palette-bg`, `-surface`, `-surface-2`, `-text`, `-muted`, `-border`, `-focus`,
  `-overlay`. Themes set these and nothing else should.
- **Semantic:** `--bg`, `--surface`, `--surface-2`, `--text`, `--muted`, `--border`, `--focus`,
  `--overlay`, computed at `:root` from the palette, plus `--accent`, `--accent-contrast`,
  `--accent-hover` and `--accent-soft`.
- **Health:** `--good`, `--caution`, `--bad`, `--unknown` with matching `-soft` tints, plus `--info`.
- **Data viz:** `--gauge-track` and `--sparkline`, kept apart from the health colours so charts stay
  readable in every theme.

## 5. Theme system

- **Mechanism:** each theme is a `[data-theme="…"]` block setting the `--palette-*` values plus its own
  accent and health colours; the semantic tokens derive from those at `:root`. Themes live in
  `tokens.css`, with no theme files to install. `auto` follows the OS; `light` and `dark` are explicit.
- **Named palettes:** `catppuccin-latte`, `catppuccin-frappe`, `catppuccin-macchiato`, `catppuccin-mocha`,
  `dracula`, `nord`, `gruvbox-light`, `gruvbox-dark`, `synthwave-light`, `synthwave-dark`, `hacker`,
  `solarized-light`, `solarized-dark`, `tokyo-night`, `one-dark`, each stating its own colours.
- **Custom:** a base (`light` or `dark`) and an accent. The server computes `--accent-contrast`, the hover
  shade and the soft tint from the accent so text on it meets WCAG AA. These rows are hidden unless the
  theme is `custom`; an inline value wins over the stylesheet, so a named palette gets none.
- **Surface tint:** `data-theme-tint` of `neutral`, `slate`, `warm` or `cool` remixes the active palette
  with `color-mix`, so it composes with any theme.
- **No flash of wrong theme:** the server renders `data-theme`, `data-theme-base` and `data-theme-tint`
  before first paint; `theme.js` only resolves `auto` and applies live preview changes. Settings: `theme`,
  `theme_base`, `theme_accent`, `theme_surface_tint`. Two tests keep the picker and the stylesheet honest:
  one asserts every `ThemeNames` entry has a palette block, the other that the picker offers exactly those
  names.

## 6. Component inventory

| Component | Notes |
|---|---|
| **Verdict** | Dot, word and colour: `Healthy`, `Caution`, `Failing`, `No reading`, plus `score N of 100` on the device page. |
| **Fleet table** | Drive (name, then model · transport · capacity · first mountpoint), Health, Usage, Temp, Powered on, then the row's own actions. Every column sorts from its header; the caret and `aria-sort` say which one and which way, and the default is the reading order. Hidden drives come back through the `N hidden` toggle as the same table, dimmed, with **Unhide**. |
| **Glance line** | Counts first, then the next thing a reader wants. On the fleet the counts are `aria-pressed` filters: click to narrow, click again to clear. |
| **Save bar** | Settings only, sticky to the window bottom, shown only while dirty, carrying state text, Revert and Save. Opaque. |
| **Theme swatches** | Nineteen chips in a `radiogroup`, each carrying its own `data-theme` so the palette resolves inside it. They never read the semantic tokens, which are computed at `:root`. |
| **Weekday strip** | Seven labelled checkboxes; selected state from `:has(input:checked)` behind `@supports selector(:has(*))`, so a browser without `:has` gets the plain row. |
| **Attribute table** | ID, name, value, worst, threshold, interpreted raw, with a `?` on every attribute the interpreter acts on. A row is badged at threshold only when the drive reports its own attribute at or below its own threshold (`A12`). Not sortable. |
| **Sparkline** | Inline SVG with current value and change across the window; hover or arrow keys show a value, date and time. Fewer than two readings and the series is dropped. |
| **Notice** | A neutral panel for something that is not a fault: no S.M.A.R.T. (with the transport override), the closed container access gate and the compose fragment that opens it, a running self-test pass. Never styled as an error. |
| **Toasts and dialogs** | Toasts auto-dismiss in an `aria-live` region. Dialogs are native `<dialog>` with `showModal`; one holding unsaved input closes only on Cancel or `Esc`, and dismissal follows where the press landed, so a drag out of a field is a text selection. |
| **Empty states** | "No drives yet, press Scan to look for them.", "No drives match.", "No schedules yet.", `Could not read …` with the message. Skeleton shapes while data loads. |
| **Setting row** | Label and one or two lines of help left, control right-aligned so every value lands in one column. Numeric rows pair a slider with a number box. |

## 7. Screen specifications

### 7.1 Fleet (`/`)

- **Header:** title, a search box (name, bay, serial, model, tag, mountpoint; focused by `/`) and a
  `Scan now` button. **Glance line:** the four state counts as filters, then `Checked 3h ago`.
- **Columns:** Drive, Health, Usage, Temp, Powered on. Health is the dot, word and score in one cell; a
  drive that is not `good` adds one line of its own reason. Usage is a track and a percentage, `n/a`
  without a filesystem; temperature and powered-on hours are right-aligned and read `n/a` when absent,
  never `0`.
- **Order:** failing, caution, no reading, healthy; within a state the lower score, then the name.
  **Rows** are focusable and open on click, `Enter` or `Space`, and a caution, failing or missing row
  carries a 2px inset rule in its state colour. Below the table: `N drives`, and links to Schedules and
  Settings. No card view, score ring or selection column survives from the retired design.

### 7.2 Device detail (`/device/<serial>`)

1. **Identity and freshness:** name, model, transport, firmware, serial in mono, the verdict with its
   score and reason, `Scan this drive` and `Export...`, then how old the data is, or "reading the drive
   now..." while the page re-reads a drive older than `device_refresh_minutes` (15, or never at `0`).
2. **Facts:** capacity, powered on, wear, temperature, mounted at, device node, bay, last scanned;
   anything the drive did not report reads `n/a`.
3. **Disk space:** usage line, disk map, partition table, the map and the table marking each other both
   ways. The map is the usage bar when a layout exists.
4. **Trends:** six cards, two rows of three: health score, temperature, power-on hours, wear, storage, bad
   sectors. Every reading draws a dot, so a gap in scanning is visible; the range picker (7d, 30d, 90d,
   All, default All) redraws only the trend host.
5. **Attributes:** the table above, aside `N reported · M at threshold`. An NVMe drive has no ATA
   table; its health log is a grid of labelled values.
6. **Self-test:** short or long, progress, abort, and the drive's own log. The only place the page reads a
   drive live, and only while a test it started is running.
7. **Drive logs** when reported, then a footer reading `Read from /dev/sda 3d ago` or `Never scanned`.

### 7.3 Schedules (`/schedules`)

- **Table:** Schedule, Runs, Next run, Last run, On, with `Run now`, `Edit`, `Delete` per row, under a
  glance line of counts and when the next one runs.
- **Editor:** an interval, chosen weekdays plus a time, or a cron expression with a plain-English preview.
  The **Action** is *Scan the drives*, *Send a digest of the period* or *Run a self-test on each drive*; a
  self-test adds a short/long picker and says it runs one drive at a time and writes to the drives.
- **Running pass:** while a self-test pass drains, a notice above the table says how many drives are done
  and which is testing now (`ADR-024`). The empty state offers the usual first schedule.

### 7.4 Notifications (`/notifications`)

A **failure banner** appears when recent deliveries failed, naming the channels, the time and the detail:
a channel that cannot deliver is a drive nobody is watching.

- **Channel table:** label and type, the events it receives, the group scope, then `Test`, `Edit`,
  `Delete`. Events are chips, and an empty subscription means every event, so the chip reads **Every
  event**. Scope is a second chip column: **Every group** when nothing is excluded, the tag names when
  something is.
- **Channel editor:** type-specific fields (ntfy server, topic, token; webhook URL, method, headers),
  event checkboxes, and a group scope of one checkbox per tag in use, offered only when the fleet has
  tags. **Test** sends one event through the settings as they stand, saved or not, closes nothing and
  writes nothing, so Save is the only thing that persists.
- **Recent deliveries** (sent, failed, squelched), and **maintenance windows**.

### 7.5 Settings (`/settings/<page>`)

Four pages reached from the sidebar: **Thresholds**, **Notifications**, **Appearance**, **Data**. They
were headings on one screen; a dozen controls split four ways is four short pages. `/settings` renders the
first.

- **Thresholds:** numeric thresholds as a slider plus a number box, the storage warning level, the
  "caution on any sector problem" switch, then the per-model table. A note says a verdict is worked out
  when a drive is read, so a threshold applies from the next scan; beside it are `Reset to recommended`
  and `Scan now and re-check`.
- **Per-model thresholds are a table, not a schema** (`A3`). One row per model, one column per threshold;
  a blank cell means "same as above", shown greyed as the placeholder. A row with overrides is marked
  `overridden` and carries `Reset`; `Load the fleet's models` adds an entry per model, and the
  **Advanced (JSON)** box holds the same data for keys no column covers (the textarea is what is saved).
- **Notifications page:** how long a condition stays quiet, how much history is kept, and the public URL
  for the "Open drive" button. **Appearance page:** temperature unit (Celsius is canonical; only
  rendering converts), the theme swatches, the custom rows, the surface tint, the refresh intervals,
  and the storage-usage toggles.
- **Data page:** one JSON file of labels, settings and schedules, exported through an anchor with
  `download` and imported behind a confirmation that states the destructive part first (a file carrying
  schedules replaces the list). Then the Prometheus toggle and the knowledge base editor, whose status
  line says how many families are known, how many shipped and how old a fetched copy is.
- **Save is per page.** Each page sends only its own fields and the server merges per key, so saving
  Appearance cannot disturb a threshold. Save is disabled until something changes, leaving with unsaved
  edits asks first, and a page loads only what it draws. There is no About card and no second version
  line: the version appears once, in the wordmark.

## 8. Interaction and feedback

- **Scan progress** arrives over SSE (`GET /api/events`); a finished scan shows a toast and re-renders in
  place. The self-test card is the one poller, while a test it started runs.
- **Keyboard:** `s` scans and `/` focuses the fleet search, both ignored in a field or dialog; `Esc`
  closes a dialog.
- **A re-render never moves the reader.** Every view empties its container and then awaits its data, and a
  briefly short page makes the browser clamp the scroll to the top. The container is held at its old
  height across the swap and released after, so a refresh or a finished scan leaves the reader where they
  were.

## 9. Accessibility

- WCAG **AA** contrast in every theme, including a custom accent (§5); colour is never the sole carrier of
  meaning, so every verdict has a dot and a word.
- Semantic HTML and ARIA: `nav` with `aria-current="page"`, real `table` elements, native `dialog` (which
  traps focus), `aria-pressed` on the fleet filters, `role="status"` plus `aria-live="polite"` on toasts,
  `role="radiogroup"` on the theme swatches.
- A visible focus ring (`2px solid var(--focus)`, 2px offset) everywhere, with `prefers-reduced-motion`
  and `prefers-color-scheme` respected.
- Attribute explanations open from a `?` button, so they are reachable without hover.

## 10. Implementation approach

- **Static shell plus vanilla ES modules:** no framework, no build step, no npm tree. `index.html` is a
  server-rendered template, the rest is modules under `assets/js`, and there are three stylesheets.
- **Theming touches only tokens** (§5), and the server renders the theme attribute, which removes the
  flash-of-wrong-theme class of bug. These specs are the contract: work is "build component X to spec",
  not "make it look nice", which is what keeps iterative UI work from drifting.

## 11. UI acceptance criteria (definition of done)

1. Every affected screen has empty, loading, scanning, error and populated states.
2. It renders in light, dark and a custom accent, with AA contrast.
3. It is usable at 360px and at desktop width.
4. It works keyboard only, with a visible focus ring.
5. It makes no external network requests.
6. No hard-coded colours, radii or spacing: tokens only.
7. Scan updates arrive over SSE, never by polling the fleet.
