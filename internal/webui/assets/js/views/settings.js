// Settings (docs/UI.md §7.5).
import { el, mount, convertTemp, toCelsius } from '../dom.js';
import { api } from '../api.js';
import { errorBox, toast } from '../components.js';
import { applyTheme } from '../theme.js';

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
    help: 'Snapshots older than this are pruned after each scan.',
    min: 7, max: 3650, step: 1, unit: 'days',
  },
  {
    key: 'notification_log_retention_days',
    label: 'Notification log retention',
    help: 'Delivery records older than this are pruned.',
    min: 1, max: 365, step: 1, unit: 'days',
  },
];

const ALL_SPECS = [...THRESHOLDS, ...OPERATIONS];

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

  const unit = el('select', {}, [
    el('option', { value: 'c', text: 'Celsius (°C)' }),
    el('option', { value: 'f', text: 'Fahrenheit (°F)' }),
  ]);
  unit.value = settings.temperature_unit === 'f' ? 'f' : 'c';

  // Switching units re-renders so the temperature slider's bounds and label match.
  // Values edited but not yet saved are carried across in Celsius, so nothing the
  // user typed is silently reinterpreted as a different temperature.
  unit.addEventListener('change', () => {
    const next = {};
    for (const spec of ALL_SPECS) {
      const raw = Number(inputs[spec.key].value);
      if (!Number.isFinite(raw)) continue;
      const celsius = spec.temperature ? toCelsius(raw, renderUnit) : raw;
      next[spec.key] = spec.temperature ? convertTemp(celsius, unit.value) : celsius;
    }
    ctx.setTemperatureUnit(unit.value);
    draw(container, ctx, data, { carried: next, unit: unit.value });
  });

  const theme = el('select', {}, ['auto', 'light', 'dark', 'custom'].map((v) => el('option', { value: v, text: v })));
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

  // Live preview while the picker is open.
  for (const control of [theme, themeBase, themeAccent, themeTint]) {
    control.addEventListener('input', () => applyTheme(currentTheme()));
  }

  const save = el('button', {
    class: 'btn btn-primary',
    type: 'button',
    text: 'Save settings',
    onclick: async (event) => {
      const payload = {
        warn_on_any_nonzero: warn.checked ? 'true' : 'false',
        temperature_unit: unit.value,
        ...currentTheme(),
      };
      for (const spec of ALL_SPECS) {
        const raw = Number(inputs[spec.key].value);
        if (!Number.isFinite(raw)) continue;
        // A temperature is stored in Celsius whatever the display unit says.
        payload[spec.key] = String(spec.temperature ? toCelsius(raw, renderUnit) : raw);
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

  const version = document.getElementById('app')?.dataset.version || 'dev';

  mount(container,
    el('div', { class: 'page-head' }, [
      el('div', {}, [
        el('h1', { text: 'Settings' }),
        el('div', { class: 'sub', text: 'Stored in the database and applied immediately — no restart needed.' }),
      ]),
      el('span', { class: 'spacer' }),
      save,
    ]),

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
      el('div', {}, operationRows),
    ]),

    el('div', { class: 'card stack' }, [
      el('h2', { text: 'Appearance' }),
      el('div', {}, [
        settingRow('Temperature unit',
          'How drive temperatures are displayed. The threshold above follows this too — the stored value stays Celsius, which is what the drives report.',
          unit),
        settingRow('Theme', 'Auto follows your operating system.', theme),
        settingRow('Custom base', 'Used only when the theme is "custom".', themeBase),
        settingRow('Accent colour', 'Recolours buttons, links and focus rings.', themeAccent),
        settingRow('Surface tint', 'Shifts every surface together; neutral leaves the palette untouched.', themeTint),
      ]),
    ]),

    el('div', { class: 'card stack' }, [
      el('h2', { text: 'About' }),
      el('div', { class: 'muted', text: `Smart-Serve ${version}` }),
      el('div', { class: 'muted', text: 'S.M.A.R.T. data comes from smartmontools; the app never writes to a drive.' }),
    ]),
  );
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
  if (!spec.temperature) return { min: spec.min, max: spec.max, step: spec.step };
  return unit === 'f' ? { min: 86, max: 194, step: 1 } : { min: 30, max: 90, step: 1 };
}

/** The stored value expressed in the render unit. */
function displayValue(spec, settings, unit) {
  const stored = Number(settings[spec.key]);
  if (!Number.isFinite(stored)) {
    return spec.temperature ? convertTemp(55, unit) : spec.min;
  }
  return spec.temperature ? convertTemp(stored, unit) : stored;
}

/** A slider paired with a number box, so a value can be dragged or typed. */
function sliderRow(spec, settings, unit, carried, inputs) {
  const bounds = boundsFor(spec, unit);
  // A value edited but not yet saved wins over the stored one.
  const value = carried[spec.key] ?? displayValue(spec, settings, unit);
  const unitLabel = spec.temperature ? `°${unit === 'f' ? 'F' : 'C'}` : (spec.unit || '');

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
