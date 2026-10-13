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
- **Named palettes:** `catppuccin-latte`, `catppuccin-mocha`, `dracula`, `nord`,
  `gruvbox-light`, `gruvbox-dark`. Each states its own `--palette-*`, accent and
  health colours, so a palette looks like itself rather than the default in a
  different shade. Their soft variants and hover shades are mixed once from the
  solid values (`color-mix`), so a new palette is a dozen lines.
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
| **Health gauge** | Circular SVG ring, 0–100 + grade. Colour = band. Hover/tap → penalty breakdown popover (§8.4 of `SMART_INTERPRETATION.md`). |
| **Device card** | Name (user), model, capacity, verdict badge, small gauge, temperature, transport icon (USB vs internal), mountpoint(s), "last scanned". Actions: open, scan-this, hide. A card whose drive is absent from the latest scan shows a *Not connected* badge and is dimmed. |
| **Devices header** | One card holding the page title, the clickable verdict counts and the search box. The counts are a filter for the grid below, so they sit beside the thing they filter instead of in a row of their own. |
| **Attribute table** | Sortable, zebra rows; ID / name / value / worst / threshold / interpreted raw / status dot; per-attribute help tooltip ("what is this?"). |
| **Sparkline / trend card** | Inline SVG per metric (health score, temperature, power-on hours, wear) with the current value and the change across the window, plus a 7 / 30 / 90 day / all range selector. A row with fewer than two readings is dropped — one point is not a trend. |
| **Export dialog** | One row per format (plain text / web page / spreadsheet / JSON) with what each is for, rather than a single button that silently picks one. |
| **Scan button** | Primary action. States: idle · scanning (spinner + "3/5 devices") · done (toast). |
| **Toasts** | Scan complete, saved, notification sent; auto-dismiss, stacked. |
| **Dialogs / sheets** | Schedule editor, channel editor, maintenance window, transport override, export, confirmations. Full-screen sheets on mobile. |
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
- **Disconnected:** a drive absent from the latest scan keeps its record — its notes
  and history must survive an unplug — but is flagged `is_missing`. Those cards are
  hidden by default; a **Show N disconnected** toggle reveals them, dimmed and
  carrying a *Not connected* badge instead of a verdict. They are excluded from the
  summary-bar counts, so a vanished drive can never silently flatter the health
  totals.

### 7.2 Device detail
- **Header:** editable inline name, model, serial, firmware, capacity, drive age.
- **Health block:** large gauge + verdict badge + plain-language rationale
  ("5 pending sectors").
- **Identity card:** model, model family, serial, firmware, capacity, interface
  speed, transport, rotation, WWN, device path.
- **Usage and media card:** power-on hours, power cycles, temperature, total data
  read / written, life remaining, mountpoints, filesystem, first seen, last scanned.
  Throughput and power figures come from the API's derived `metrics` block
  (`API.md`), so ATA and NVMe drives read identically.
- **Mountpoints / Filesystem:** taken from the disk's *partitions* — a whole-disk
  node is never mounted itself (`DATA_MODEL.md`). Filesystem shows every distinct
  type found, e.g. `ext4, vfat`.
- **Attribute table** (§6) — for NVMe, the full health log: critical warning,
  spare, percentage used, data units read/written (count **and** humanised size),
  host read/write commands, controller busy time, power cycles/hours, unsafe
  shutdowns, media errors, error-log entries, warning/critical temperature time.
  A "show raw `smartctl` output" toggle sits alongside it.
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
- Channel editor: type-specific fields (ntfy server/topic/token; webhook URL/method/
  headers); event-subscription checkboxes.
- Recent-deliveries log (sent / failed / squelched).

### 7.5 Settings
- Grouped sections: **Health thresholds**, **Alerts and retention**, **Appearance**,
  **About**.
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
- **About** shows the version and notes that the app never writes to a drive.

## 8. Interaction & feedback

- **Optimistic UI** for all mutations (notes, schedules, settings) with rollback +
  error toast on failure.
- **Scan progress** is SSE-driven (`GET /api/events`); the UI never polls.
- **Keyboard shortcuts** (desktop): `s` = scan, `/` = focus filter, `Esc` = close
  dialog, `?` = shortcut help.
- **Reduced motion** honoured via `prefers-reduced-motion`.
- **No layout shift** during scans — cards reserve their space (skeletons match final
  dimensions).

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


