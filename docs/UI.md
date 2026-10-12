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
- Device cards in a responsive grid: `repeat(auto-fill, minmax(320px, 1fr))`.
- The attribute table is horizontally scrollable on narrow screens rather than
  collapsing into unreadable stacked cells.
- Max content width ~1200px on very large displays; content centred.
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

## 5. Theme system (dark / light / custom colour themes)

Theming is a **token-swap**, never a component change. This is the key to shipping a
theme picker without touching any component code.

- **Mechanism:** tokens are defined on `[data-theme="light"]`, `[data-theme="dark"]`,
  and `[data-theme="custom"]` selectors; components only ever reference `var(--…)`.
- **Built-in themes:** `auto` (follows the OS via `prefers-color-scheme`), `light`,
  `dark`.
- **Custom theme:** the user picks an **accent colour** and a **base** (light or
  dark). Derived tokens (`--accent-contrast`, hover/active shades, focus ring) are
  computed from the accent's luminance so text on the accent always meets WCAG AA
  contrast automatically. Optional surface tint (slate / neutral / warm / cool).
- **No theme files:** a theme is a handful of token values stored in settings —
  nothing to install, nothing to break.
- **No flash of wrong theme:** the server renders `<html data-theme="…">` from the
  stored setting, so the correct theme is applied before first paint (no JS-only
  theming). A tiny inline script only handles `auto` → OS resolution.
- **Persistence:** `theme`, `theme_base`, `theme_accent` settings
  (`DATA_MODEL.md`); applied per-browser via a cookie for unauthenticated MVP so
  multiple users/devices can differ, with the server value as the default.

## 6. Component inventory

| Component | Notes |
|---|---|
| **Verdict badge** | Pill: icon + word + colour (`good`/`caution`/`bad`/`unknown`). Colour is never the only signal. |
| **Health gauge** | Circular SVG ring, 0–100 + grade. Colour = band. Hover/tap → penalty breakdown popover (§8.4 of `SMART_INTERPRETATION.md`). |
| **Device card** | Name (user), model, capacity, verdict badge, small gauge, temperature, transport icon (USB vs internal), mountpoint(s), "last scanned". Actions: open, scan-this, hide. |
| **Summary bar** | Counts by verdict + total; clicking filters the grid. |
| **Attribute table** | Sortable, zebra rows; ID / name / value / worst / threshold / interpreted raw / status dot; per-attribute help tooltip ("what is this?"). |
| **Sparkline** | Inline SVG trend for temp / sectors / wear (Phase 2). |
| **Scan button** | Primary action. States: idle · scanning (spinner + "3/5 devices") · done (toast). |
| **Toasts** | Scan complete, saved, notification sent; auto-dismiss, stacked. |
| **Dialogs / sheets** | Schedule editor, channel editor, transport override, confirmations. Full-screen sheets on mobile. |
| **Empty states** | "No scan yet — press Scan to discover drives", "No devices found", "No schedules". |
| **Skeletons** | Placeholder shapes while data loads — never a blank screen. |
| **Settings forms** | Inline validation; each field shows its **effective value, default, and source** (`env` or `db`, per `ADR-009`). |

## 7. Screen specifications

### 7.1 Dashboard
- Summary bar (verdict counts) → device grid → global Scan button (top-right, sticky).
- **Empty:** onboarding message + prominent Scan button.
- **Loading:** skeleton cards.
- **Scanning:** Scan button shows progress; cards update live via SSE as each device
  completes (no polling — `ADR-006`).
- **Populated:** cards sorted by severity (bad → caution → good), then by user name.

### 7.2 Device detail
- **Header:** editable inline name, model, serial, firmware, capacity, drive age.
- **Health block:** large gauge + verdict badge + plain-language rationale
  ("5 pending sectors").
- **Key stats:** temperature, power-on hours, power cycles, interface speed,
  transport.
- **Attribute table** (§6) with a "show raw `smartctl` output" toggle.
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
- Grouped sections: **Thresholds** (with defaults shown), **Scan options**
  (timeout, excludes, missed-run policy), **Appearance** (theme picker),
  **About** (version, image arch, smartctl version, knowledge-base status).
- Every field displays effective value + source (§6).

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


