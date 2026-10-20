// Settings (docs/UI.md §7.5).
import { el, mount, convertTemp, toCelsius, convertTempDelta, toCelsiusDelta } from '../dom.js';
import { api } from '../api.js';
import { errorBox, toast, confirmDialog } from '../components.js';
import { applyTheme } from '../theme.js';
import { icon } from '../icons.js';

// Themes offered in Appearance. Kept in step with ThemeNames in
// internal/webui/webui.go — a test on each side pins the pair together.
const THEMES = [
  ['auto', 'Auto (follows your system)'],
  ['light', 'Light'],
  ['dark', 'Dark'],
  ['catppuccin-latte', 'Catppuccin Latte'],
  ['catppuccin-frappe', 'Catppuccin Frappé'],
  ['catppuccin-macchiato', 'Catppuccin Macchiato'],
  ['catppuccin-mocha', 'Catppuccin Mocha'],
  ['dracula', 'Dracula'],
  ['nord', 'Nord'],
  ['gruvbox-light', 'Gruvbox Light'],
  ['gruvbox-dark', 'Gruvbox Dark'],
  ['synthwave-light', 'Synthwave Light'],
  ['synthwave-dark', "Synthwave '84"],
  ['hacker', 'Hacker'],
  ['solarized-light', 'Solarized Light'],
  ['solarized-dark', 'Solarized Dark'],
  ['tokyo-night', 'Tokyo Night'],
  ['one-dark', 'One Dark'],
  ['custom', 'Custom (pick your own accent)'],
];

/** The theme values the picker offers, in order. */
export function themeNames() {
  return THEMES.map(([value]) => value);
}

// Numeric settings are edited with a slider plus a number box: drag to explore,
// type for precision. `min`/`max`/`step` describe the slider. A temperature is
// expressed in the *display* unit and converted back to Celsius on save, because
// Celsius is what the drives report and what the database stores.
const THRESHOLDS = [
  {
    key: 'temperature_threshold_c',
    label: 'Temperature caution threshold',
    help: 'A drive at or above this is flagged, and a scheduled scan sends an alert.',
    temperature: true,
  },
  {
    key: 'reallocated_bad_threshold',
    label: 'Reallocated sectors → failing',
    help: 'Attribute 5 at or above this raw count fails the drive.',
    min: 1, max: 50, step: 1,
  },
  {
    key: 'pending_bad_threshold',
    label: 'Pending sectors → failing',
    help: 'Attribute 197 at or above this raw count fails the drive.',
    min: 1, max: 50, step: 1,
  },
  {
    key: 'uncorrectable_bad_threshold',
    label: 'Uncorrectable sectors → failing',
    help: 'Attribute 198 at or above this raw count fails the drive.',
    min: 1, max: 50, step: 1,
  },
  {
    key: 'nvme_percentage_used_warn',
    label: 'NVMe endurance used → caution',
    help: 'Endurance consumed before an NVMe drive is flagged.',
    min: 50, max: 100, step: 1, unit: '%',
  },
];

const OPERATIONS = [
  {
    key: 'squelch_seconds',
    label: 'Alert squelch window',
    help: 'How long a persisting condition stays quiet before it alerts again.',
    min: 60, max: 86400, step: 60, unit: 'seconds',
  },
  {
    key: 'retention_days',
    label: 'History retention',
    help: 'Snapshots older than this are pruned after each scan. It also sets how far back the ' +
      'trend and longevity estimates look, so a longer retention is a longer view as well as a ' +
      'longer memory.',
    min: 7, max: 3650, step: 1, unit: 'days',
  },
  {
    key: 'notification_log_retention_days',
    label: 'Notification log retention',
    help: 'Delivery records older than this are pruned.',
    min: 1, max: 365, step: 1, unit: 'days',
  },
  // Correlated change (FEATURES.md A25). Two numbers, both configurable because the fleet size this
  // has to suit spans an order of magnitude (ADR-023).
  {
    key: 'correlation_min_drives',
    label: 'Correlated heat: drives that must move',
    help: 'How many drives in one tag group have to heat up together before the app reports it as ' +
      'one finding about the group rather than several about the drives. Raise it for a big ' +
      'enclosure: 4 is a sixth of a 24-bay NAS and nearly all of a workstation.',
    min: 2, max: 24, step: 1, unit: 'drives',
  },
  {
    key: 'correlation_delta_c',
    label: 'Correlated heat: the rise that counts',
    help: 'How far a drive has to rise between two readings to count as having moved. A stopped ' +
      'fan moves a whole chassis well past this; a warm afternoon usually does not.',
    // A span, not a reading: it scales with the unit but does not take the 32° offset.
    delta: true,
  },
];

const ALL_SPECS = [...THRESHOLDS, ...OPERATIONS];

/**
 * Whether a setting's value is a temperature, and so follows the display unit.
 *
 * `temperature` is a *reading* and converts with the offset (30 °C is 86 °F); `delta` is a *span*
 * and scales without it (a 8 °C rise is a 14 °F rise, not a 46 °F one). Both follow the unit, because
 * "eight degrees hotter" is the same physical change either way — and getting the second one wrong
 * would make the slider say 46 °F for a threshold that means 14.
 */
const isTemperature = (spec) => Boolean(spec.temperature || spec.delta);

/** A stored (Celsius) value in the unit the control is showing. */
function toDisplay(spec, celsius, unit) {
  if (spec.delta) return convertTempDelta(celsius, unit);
  return spec.temperature ? convertTemp(celsius, unit) : celsius;
}

/** A value typed into the control, back to what gets stored. */
function fromDisplay(spec, shown, unit) {
  if (spec.delta) return toCelsiusDelta(shown, unit);
  return spec.temperature ? toCelsius(shown, unit) : shown;
}

const DEFAULTS = {
  temperature_threshold_c: '55',
  reallocated_bad_threshold: '10',
  pending_bad_threshold: '10',
  uncorrectable_bad_threshold: '10',
  warn_on_any_nonzero: 'true',
  nvme_percentage_used_warn: '90',
  retention_days: '180',
  notification_log_retention_days: '30',
  squelch_seconds: '3600',
  correlation_min_drives: '4',
  correlation_delta_c: '8',
  public_url: '',
  temperature_unit: 'c',
  theme: 'auto',
  theme_base: 'dark',
  theme_accent: '#3b82f6',
  theme_surface_tint: 'neutral',
};

export async function render(container, ctx) {
  mount(container, el('div', { class: 'skeleton', style: 'width:30%;height:24px' }));
  try {
    draw(container, ctx, await api.settings());
  } catch (err) {
    mount(container, errorBox(`Could not load settings: ${err.message}`));
  }
}

function draw(container, ctx, data, { carried = {}, unit: unitOverride } = {}) {
  const settings = { ...DEFAULTS, ...(data.settings || {}) };

  // The stored settings are authoritative for this page, so the sliders agree with
  // the select even if the unit was changed elsewhere since the page loaded. The
  // override is what the unit picker passes when it re-renders.
  const renderUnit = unitOverride ?? (settings.temperature_unit === 'f' ? 'f' : 'c');
  const inputs = {};

  const thresholdRows = THRESHOLDS.map((spec) => sliderRow(spec, settings, renderUnit, carried, inputs));
  const operationRows = OPERATIONS.map((spec) => sliderRow(spec, settings, renderUnit, carried, inputs));

  const warn = el('input', { type: 'checkbox', checked: settings.warn_on_any_nonzero === 'true' });

  // Where this deployment is reachable from wherever an alert lands (`FEATURES.md` B10), so a
  // channel that supports action buttons can offer a link back. Free text, and empty by default:
  // the app cannot know its own external address, and guessing one would produce a link that works
  // for nobody while looking like a feature.
  const publicUrl = el('input', {
    type: 'url',
    class: 'setting-text',
    placeholder: 'https://nas.example.lan:8080',
    value: settings.public_url || '',
    'aria-label': 'Public URL',
  });

  const unit = el('select', {}, [
    el('option', { value: 'c', text: 'Celsius (°C)' }),
    el('option', { value: 'f', text: 'Fahrenheit (°F)' }),
  ]);
  // renderUnit, not the stored setting: this page re-renders on a unit change, and
  // reading the stored value back would snap the picker to Celsius the instant the
  // user chose Fahrenheit — and Save would then persist the wrong unit.
  unit.value = renderUnit;

  // Switching units re-renders so the temperature slider's bounds and label match.
  // Values edited but not yet saved are carried across in Celsius, so nothing the
  // user typed is silently reinterpreted as a different temperature.
  unit.addEventListener('change', () => {
    const next = {};
    for (const spec of ALL_SPECS) {
      const raw = Number(inputs[spec.key].value);
      if (!Number.isFinite(raw)) continue;
      const celsius = fromDisplay(spec, raw, renderUnit);
      next[spec.key] = toDisplay(spec, celsius, unit.value);
    }
    ctx.setTemperatureUnit(unit.value);
    draw(container, ctx, data, { carried: next, unit: unit.value });
  });

  const theme = el('select', {}, THEMES.map(([value, text]) => el('option', { value, text })));
  theme.value = settings.theme;
  const themeBase = el('select', {}, ['dark', 'light'].map((v) => el('option', { value: v, text: v })));
  themeBase.value = settings.theme_base;
  const themeAccent = el('input', { type: 'color', value: settings.theme_accent });
  const themeTint = el('select', {}, ['neutral', 'slate', 'warm', 'cool'].map((v) => el('option', { value: v, text: v })));
  themeTint.value = settings.theme_surface_tint;

  const currentTheme = () => ({
    theme: theme.value,
    theme_base: themeBase.value,
    theme_accent: themeAccent.value,
    theme_surface_tint: themeTint.value,
  });

  // The base and accent only affect the custom theme — a named palette owns its own
  // — so those two rows are hidden rather than sitting there doing nothing.
  const themeRow = settingRow('Theme',
    'Named palettes ship with the app. Auto follows your operating system.', theme);
  const customRows = [
    settingRow('Custom base', 'Light or dark foundations for a custom theme.', themeBase),
    settingRow('Accent colour', 'Recolours buttons, links and focus rings.', themeAccent),
  ];
  const tintRow = settingRow('Surface tint',
    'Shifts every surface together; neutral leaves the palette untouched.', themeTint);
  const syncThemeRows = () => {
    for (const row of customRows) row.hidden = theme.value !== 'custom';
  };

  // Live preview while the picker is open.
  for (const control of [theme, themeBase, themeAccent, themeTint]) {
    control.addEventListener('input', () => {
      syncThemeRows();
      applyTheme(currentTheme());
    });
  }
  syncThemeRows();

  const save = el('button', {
    class: 'btn btn-primary',
    type: 'button',
    text: 'Save settings',
    onclick: async (event) => {
      const payload = {
        warn_on_any_nonzero: warn.checked ? 'true' : 'false',
        temperature_unit: unit.value,
        public_url: publicUrl.value.trim(),
        ...currentTheme(),
      };
      for (const spec of ALL_SPECS) {
        const raw = Number(inputs[spec.key].value);
        if (!Number.isFinite(raw)) continue;
        // A temperature is stored in Celsius whatever the display unit says.
        payload[spec.key] = String(fromDisplay(spec, raw, renderUnit));
      }

      event.currentTarget.disabled = true;
      try {
        await api.putSettings(payload);
        ctx.setTemperatureUnit(payload.temperature_unit);
        applyTheme(payload);
        toast('Settings saved', 'good');
      } catch (err) {
        toast(err.message, 'error');
      } finally {
        event.currentTarget.disabled = false;
      }
    },
  });

  mount(container,
    el('div', { class: 'page-head' }, [
      el('div', {}, [
        el('h1', { text: 'Settings' }),
        el('div', { class: 'sub', text: 'Stored in the database and applied immediately, so no restart is needed.' }),
      ]),
      el('span', { class: 'spacer' }),
      save,
    ]),

    // One grid for all three cards: auto-fit decides how many columns fit, so a wide
    // screen gets three side by side and a narrow one stacks them. That is what keeps
    // the gap between a label and its control from stretching across the page.
    el('div', { class: 'panel-grid' }, [
      el('div', { class: 'card stack' }, [
        el('h2', { text: 'Health thresholds' }),
        el('div', {}, [
          ...thresholdRows,
          settingRow('Caution on any sector problem',
            'Flag a drive as soon as a single reallocated, pending or uncorrectable sector appears.',
            warn),
        ]),
      ]),

      el('div', { class: 'card stack' }, [
        el('h2', { text: 'Alerts and retention' }),
        el('div', {}, [
          ...operationRows,
          settingRow('Public URL',
            'Where this app is reachable from outside the host, used to put an "Open drive" button ' +
            'on a notification. Leave it empty and no button is added — the app cannot know its own ' +
            'external address, and a wrong link is worse than none.',
            publicUrl),
        ]),
      ]),

      el('div', { class: 'card stack' }, [
        el('h2', { text: 'Appearance' }),
        el('div', {}, [
          settingRow('Temperature unit',
            'How drive temperatures are displayed. The threshold above follows this too. The stored value stays Celsius, which is what the drives report.',
            unit),
          themeRow,
          ...customRows,
          tintRow,
        ]),
      ]),
      el('div', { class: 'card stack' }, [
        el('h2', { text: 'Import and export' }),
        el('p', { class: 'muted', text: 'Device labels, settings and schedules, as one JSON file. ' +
          'For moving to another host: names, bays and tags live in the database, which survives a ' +
          'restart but not a move.' }),
        el('p', { class: 'muted', text: 'Notification channels are deliberately not included. Their ' +
          'config holds tokens and passwords, and a backup file is exactly the thing that gets ' +
          'emailed around.' }),
        el('div', { class: 'row', style: 'gap:var(--space-2)' }, [
          // An anchor with `download`, not a button: this is a file to keep, and the browser's own
          // save dialog is the right one to use.
          el('a', {
            class: 'btn', href: api.configExportUrl(), download: 'smart-serve-config.json',
          }, [icon('download'), 'Export']),
          el('button', {
            class: 'btn', type: 'button',
            onclick: (e) => importConfig(ctx, e.currentTarget),
          }, ['Import']),
        ]),
      ]),
    ]),
  );
}

/**
 * Import a bundle (`FEATURES.md` C7).
 *
 * Confirmed before the file picker rather than after: the user should know what the file will do
 * before they spend time finding it, and the one destructive part — schedules are replaced rather
 * than merged — is the thing they most need to know.
 */
async function importConfig(ctx, button) {
  const ok = await confirmDialog(
    'Import a configuration',
    'Labels, settings and schedules from the file are applied. Anything the file does not mention ' +
    'is left alone, but a file that carries schedules *replaces* the schedule list. Notification ' +
    'channels are not affected.',
    'Choose a file',
  );
  if (!ok) return;

  const input = el('input', { type: 'file', accept: 'application/json,.json', hidden: true });
  input.addEventListener('change', async () => {
    const file = input.files && input.files[0];
    if (!file) {
      input.remove();
      return;
    }
    button.disabled = true;
    try {
      const applied = await api.importConfig(JSON.parse(await file.text()));
      const a = (applied && applied.applied) || {};
      toast(`Imported ${a.devices || 0} sets of labels, ${a.settings || 0} settings and ` +
        `${a.schedules || 0} schedules`, 'good');
      ctx.refresh();
    } catch (err) {
      // A malformed file is the common failure here, and "Unexpected token < in JSON" is not an
      // explanation of what went wrong.
      toast(`Could not import that file: ${err.message}`, 'error');
      button.disabled = false;
    } finally {
      input.remove();
    }
  });

  document.body.append(input);
  input.click();
}

// --- row builders --------------------------------------------------------------

/** A label and its explanation on the left, the control right-aligned opposite. */
function settingRow(label, help, control) {
  return el('div', { class: 'setting-row' }, [
    el('div', { class: 'setting-text' }, [
      el('div', { class: 'setting-label', text: label }),
      help ? el('div', { class: 'setting-help', text: help }) : null,
    ]),
    el('div', { class: 'setting-control' }, [control]),
  ]);
}

/**
 * Slider bounds for a setting, in the unit the control is showing. Fahrenheit
 * needs Fahrenheit bounds, or the temperature threshold would be unadjustable in
 * the very unit the user chose to see.
 */
function boundsFor(spec, unit) {
  // A span's bounds are a span: the 32° offset must not appear here either, or the slider would
  // start at 34 °F for a threshold of 1 °C.
  if (spec.delta) return unit === 'f' ? { min: 2, max: 45, step: 1 } : { min: 1, max: 25, step: 1 };
  if (!spec.temperature) return { min: spec.min, max: spec.max, step: spec.step };
  return unit === 'f' ? { min: 86, max: 194, step: 1 } : { min: 30, max: 90, step: 1 };
}

/** The stored value expressed in the render unit. */
function displayValue(spec, settings, unit) {
  const stored = Number(settings[spec.key]);
  if (!Number.isFinite(stored)) {
    // The documented defaults, so an install that has never opened this page shows what the app is
    // actually using rather than the slider's minimum.
    if (spec.delta) return toDisplay(spec, 8, unit);
    return spec.temperature ? convertTemp(55, unit) : spec.min;
  }
  return toDisplay(spec, stored, unit);
}

/** A slider paired with a number box, so a value can be dragged or typed. */
function sliderRow(spec, settings, unit, carried, inputs) {
  const bounds = boundsFor(spec, unit);
  // A value edited but not yet saved wins over the stored one.
  const value = carried[spec.key] ?? displayValue(spec, settings, unit);
  const unitLabel = isTemperature(spec) ? `°${unit === 'f' ? 'F' : 'C'}` : (spec.unit || '');

  const range = el('input', {
    type: 'range', class: 'setting-range',
    min: String(bounds.min), max: String(bounds.max), step: String(bounds.step),
    value: String(value), 'aria-label': spec.label,
  });
  const number = el('input', {
    type: 'number', class: 'setting-number',
    min: String(bounds.min), max: String(bounds.max), step: String(bounds.step),
    value: String(value), 'aria-label': `${spec.label} value`,
  });

  range.addEventListener('input', () => { number.value = range.value; });
  number.addEventListener('input', () => {
    const n = Number(number.value);
    if (Number.isFinite(n) && n >= bounds.min && n <= bounds.max) range.value = String(n);
  });

  inputs[spec.key] = number;

  return el('div', { class: 'setting-row' }, [
    el('div', { class: 'setting-text' }, [
      el('div', { class: 'setting-label', text: spec.label }),
      el('div', { class: 'setting-help', text: spec.help }),
    ]),
    el('div', { class: 'setting-control' }, [
      range,
      number,
      unitLabel ? el('span', { class: 'setting-unit', text: unitLabel }) : null,
    ]),
  ]);
}
