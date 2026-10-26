// Settings (docs/UI.md §7.5).
import { el, mount, convertTemp, toCelsius, convertTempDelta, toCelsiusDelta, fmtAgo } from '../dom.js';
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
    help: 'Snapshots older than this are pruned. It also sets how far back the trend and longevity ' +
      'estimates look — a longer retention is a longer view as well as a longer memory.',
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
    help: 'How many drives in one tag group must heat up together before the app reports one ' +
      'finding about the group instead of several about the drives. Raise it for a big enclosure.',
    min: 2, max: 24, step: 1, unit: 'drives',
  },
  {
    key: 'correlation_delta_c',
    label: 'Correlated heat: the rise that counts',
    help: 'The rise between two readings that counts as a drive having moved. A stopped fan moves a ' +
      'whole chassis past this; a warm afternoon does not.',
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
  prometheus_enabled: 'false',
  knowledge_url: '',
  temperature_unit: 'c',
  theme: 'auto',
  theme_base: 'dark',
  theme_accent: '#3b82f6',
  theme_surface_tint: 'neutral',
};

export async function render(container, ctx) {
  mount(container, el('div', { class: 'skeleton', style: 'width:30%;height:24px' }));
  try {
    // Both in one round trip: the knowledge card is part of the page, and a second request that
    // arrived after the first render would make the page jump.
    const [settings, knowledge] = await Promise.all([api.settings(), api.knowledge()]);
    draw(container, ctx, { ...settings, knowledge });
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

  // Off by default (`FEATURES.md` C6): the endpoint costs nothing by itself, but turning it on invites
  // another service to poll this app on a schedule, and that is the operator's decision rather than a
  // default. The same reasoning as a self-test being opt-in.
  const prometheus = el('input', { type: 'checkbox', checked: settings.prometheus_enabled === 'true' });

  // Where the knowledge base refreshes from (`FEATURES.md` A21). Empty means the documented default,
  // which points at the file in the repository — the same file this build ships.
  const knowledgeUrl = el('input', {
    type: 'url',
    class: 'setting-text',
    placeholder: 'defaults to the published table',
    value: settings.knowledge_url || '',
    'aria-label': 'Knowledge base URL',
  });

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
        prometheus_enabled: prometheus.checked ? 'true' : 'false',
        knowledge_url: knowledgeUrl.value.trim(),
        ...currentTheme(),
      };
      for (const spec of ALL_SPECS) {
        const raw = Number(inputs[spec.key].value);
        if (!Number.isFinite(raw)) continue;
        // A temperature is stored in Celsius whatever the display unit says.
        payload[spec.key] = String(fromDisplay(spec, raw, renderUnit));
      }

      // Captured before the first `await`. The spec nulls `event.currentTarget` the moment
      // dispatch ends, so reading it afterwards throws a TypeError into an unhandled promise —
      // silently, because nobody awaits a click handler — and the button stays disabled for the
      // rest of the session. One click on Save, then the page is unusable.
      const button = event.currentTarget;
      button.disabled = true;
      try {
        await api.putSettings(payload);
        ctx.setTemperatureUnit(payload.temperature_unit);
        applyTheme(payload);
        toast('Settings saved', 'good');
      } catch (err) {
        toast(err.message, 'error');
      } finally {
        button.disabled = false;
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

    // One sheet with sections, not four cards in a grid.
    //
    // The grid meant four different card heights and a ragged bottom, and reaching the shortest one
    // meant scrolling past the tallest — so it did not make the page shorter, it made it uneven. A
    // single surface whose sections lay their rows out in two columns does both jobs: it halves the
    // height of the longest section, and there is nothing left to be uneven.
    el('div', { class: 'card stack panel-wide settings-sheet' }, [
      settingsSection('Health thresholds', [
        ...thresholdRows,
        settingRow('Caution on any sector problem',
          'Flag a drive as soon as a single reallocated, pending or uncorrectable sector appears.',
          warn),
      ]),

      settingsSection('Alerts and retention', [
        ...operationRows,
        settingRow('Public URL',
          'Where this app is reachable from outside the host, for the "Open drive" button on a ' +
          'notification. Empty adds no button — the app cannot know its own address, and a wrong ' +
          'link is worse than none.',
          publicUrl),
        settingRow('Prometheus metrics',
          'Serve /metrics for a Prometheus scrape. Off by default: the endpoint costs nothing, but ' +
          'turning it on invites another service to poll this app.',
          prometheus),
      ]),

      settingsSection('Appearance', [
        settingRow('Temperature unit',
          'How temperatures are displayed; the threshold above follows it. The stored value stays ' +
          'Celsius, which is what the drives report.',
          unit),
        themeRow,
        ...customRows,
        tintRow,
      ]),

      settingsSection('Import and export', [
        el('p', { class: 'muted', text: 'Device labels, settings and schedules, as one JSON file. For ' +
          'moving to another host: names, bays and tags live in the database, which survives a ' +
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

      // Read from `data`, not from the local `settings` map — that one is the stored settings, and
      // the knowledge status is its own payload.
      knowledgeSection(data.knowledge, ctx, knowledgeUrl),
    ]),
  );
}

/**
 * One section of the settings sheet: a heading, a rule, and its rows.
 *
 * The rows are laid out by the stylesheet rather than here, because how many columns fit is a
 * question about the window and this file has no business knowing the answer.
 */
function settingsSection(title, nodes) {
  return el('section', { class: 'settings-section' }, [
    el('h2', { text: title }),
    el('div', { class: 'settings-rows' }, nodes),
  ]);
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
/**
 * The knowledge base card (`FEATURES.md` A21).
 *
 * Three layers, in the order they win: what ships with this build, a copy fetched from a URL, and the
 * entries typed here. The card exists because the last two are the *user's* — the whole point of the
 * feature is that the data outlives the repository, which means whoever runs this has to be able to
 * maintain it without a pull request.
 *
 * The refresh is a button rather than a schedule, and the card says how old the copy is instead of
 * hiding it: the one thing worse than stale data is stale data you believe is current.
 *
 * The editor is a text box rather than a form, and that is deliberate. The format *is* the interface —
 * the same JSON the bundled file and the refresh URL use — so there is one thing to learn rather than
 * a form that can only express what the form thought of. The parse error comes back verbatim and is
 * shown beside the box, which is the only place a message about a typo is worth anything.
 */
function knowledgeSection(knowledge, ctx, urlInput) {
  const k = knowledge || {};
  const error = el('div', { class: 'knowledge-error', hidden: true });
  const editor = el('textarea', {
    rows: '7',
    class: 'knowledge-editor',
    'aria-label': 'Your knowledge base entries',
    placeholder: '{\n  "version": 1,\n  "drives": [\n    { "family": "My Drive 9000", "rated_tbw": 600, "source": "the datasheet in the box" }\n  ]\n}',
    value: (k.overrides && k.overrides.payload) || '',
  });

  function showError(message) {
    error.textContent = message;
    error.hidden = !message;
  }

  /** Run an action with the button disabled and its error shown inline. */
  function act(button, run) {
    return async () => {
      showError('');
      button.disabled = true;
      try {
        await run();
      } catch (err) {
        // Inline rather than a toast: these messages name a URL and a JSON field, and a toast
        // disappears before anyone can read them.
        showError(err.message);
      } finally {
        button.disabled = false;
      }
    };
  }

  // The status line is the honest part of this card: how many families are known, how many shipped,
  // how old the fetched copy is. A reader who cannot see the age cannot decide whether to refresh.
  const parts = [`${k.resolved_entries || 0} families known`,
    `${k.bundled_entries || 0} shipped with this build`];
  if (k.cache) {
    const age = k.cache.fetched_at ? fmtAgo(k.cache.fetched_at) : 'at an unknown time';
    parts.push(`${k.cache.entries} fetched ${age}`);
  } else {
    parts.push('nothing fetched yet');
  }
  if (k.overrides) parts.push(`${k.overrides.entries} yours`);

  const refresh = el('button', { class: 'btn btn-sm', type: 'button', text: 'Refresh now' });
  refresh.addEventListener('click', act(refresh, async () => {
    const got = await api.refreshKnowledge();
    toast(`Fetched ${got.entries} families`, 'good');
    ctx.refresh();
  }));

  const save = el('button', { class: 'btn btn-sm', type: 'button', text: 'Save my entries' });
  save.addEventListener('click', act(save, async () => {
    const got = await api.putKnowledgeOverrides(editor.value);
    toast(got.entries ? `Saved ${got.entries} entries` : 'Cleared your entries', 'good');
    ctx.refresh();
  }));

  const discard = el('button', {
    class: 'btn btn-sm btn-ghost', type: 'button', text: 'Discard the fetched copy',
  });
  discard.addEventListener('click', act(discard, async () => {
    await api.clearKnowledgeCache();
    toast('Fetched copy discarded', 'good');
    ctx.refresh();
  }));

  return el('section', { class: 'settings-section knowledge' }, [
    // The heading line carries the status: how many families are known and how old the fetched copy
    // is, is what you want to know *before* deciding whether to press Refresh, and a line of its own
    // for it was a line the reader had to travel. The old paragraph explaining the feature is gone —
    // "every figure names its source" is the one thing in it that is not visible from the card.
    el('div', { class: 'row wrap' }, [
      el('h2', { text: 'Knowledge base' }),
      el('div', { class: 'muted knowledge-status', text: parts.join(' · ') }),
      el('span', { class: 'spacer' }),
      el('span', { class: 'muted', text: 'every figure names its source' }),
    ]),
    k.cache_stale
      ? el('p', { class: 'muted knowledge-warn', text: 'The stored copy came from a different URL ' +
          'than the one below. Refresh to replace it, or discard it to fall back to the shipped ' +
          'table.' })
      : null,
    el('div', { class: 'settings-rows' }, [
      settingRow('Refresh URL',
        'Where to fetch an updated table from. Empty uses the published one, which is the file this ' +
        'build already ships. Nothing is fetched on a schedule — only when you press Refresh.',
        urlInput),
    ]),
    el('div', { class: 'row knowledge-actions' }, [
      refresh,
      k.cache ? discard : null,
      // The shipped document, verbatim. This is how somebody starts their own copy to point the URL
      // at — which is the whole reason the refresh exists.
      el('a', {
        class: 'btn btn-sm btn-ghost',
        href: `data:application/json;charset=utf-8,${encodeURIComponent(k.bundled || '{}')}`,
        download: 'smart-serve-knowledge.json',
        title: 'The table this build ships, to edit and host yourself',
      }, [icon('download'), 'Download the shipped table']),
    ]),
    el('div', { class: 'field' }, [
      el('label', { text: 'Your own entries' }),
      el('p', { class: 'muted field-hint', text: 'The same format as the table above, and they win ' +
        'over it. An entry *replaces* the one for that family rather than merging with it, so copy ' +
        'across anything you want to keep. These travel in the config export.' }),
      editor,
    ]),
    error,
    el('div', { class: 'row knowledge-actions' }, [
      save,
      el('button', {
        class: 'btn btn-sm btn-ghost', type: 'button', text: 'Clear',
        onclick: () => { editor.value = ''; showError(''); },
      }),
    ]),
  ]);
}

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
