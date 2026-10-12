// Settings (docs/UI.md §7.5).
import { el, mount } from '../dom.js';
import { api } from '../api.js';
import { errorBox, toast } from '../components.js';
import { applyTheme } from '../theme.js';

const NUMERIC = [
  ['temperature_threshold_c', 'Temperature caution threshold (°C)'],
  ['reallocated_bad_threshold', 'Reallocated sectors → Bad'],
  ['pending_bad_threshold', 'Pending sectors → Bad'],
  ['uncorrectable_bad_threshold', 'Uncorrectable sectors → Bad'],
  ['nvme_percentage_used_warn', 'NVMe percentage used → Caution'],
  ['retention_days', 'History retention (days)'],
  ['notification_log_retention_days', 'Notification log retention (days)'],
  ['squelch_seconds', 'Alert squelch window (seconds)'],
  ['command_timeout_seconds', 'Command timeout (seconds)'],
];

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
  command_timeout_seconds: '15',
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

function draw(container, ctx, data) {
  const settings = { ...DEFAULTS, ...(data.settings || {}) };

  const numberInputs = {};
  const numericFields = NUMERIC.map(([key, label]) => {
    const input = el('input', { type: 'number', min: '0', value: settings[key] ?? '' });
    numberInputs[key] = input;
    return el('div', { class: 'field' }, [el('label', { text: label }), input]);
  });

  const warn = el('input', { type: 'checkbox', checked: settings.warn_on_any_nonzero === 'true' });

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
      const payload = { warn_on_any_nonzero: warn.checked ? 'true' : 'false', ...currentTheme() };
      for (const [key] of NUMERIC) payload[key] = String(numberInputs[key].value).trim();
      event.currentTarget.disabled = true;
      try {
        await api.putSettings(payload);
        toast('Settings saved', 'good');
        applyTheme(payload);
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
        el('div', { class: 'sub', text: 'Thresholds are stored in the database and can be changed here at any time.' }),
      ]),
      el('span', { class: 'spacer' }),
      save,
    ]),

    el('div', { class: 'card stack' }, [
      el('h2', { text: 'Interpretation thresholds' }),
      el('div', { class: 'form-grid' }, numericFields),
      el('label', { class: 'field-row' }, [warn, el('span', { text: 'Caution when any sector counter is non-zero' })]),
    ]),

    el('div', { class: 'card stack' }, [
      el('h2', { text: 'Appearance' }),
      el('div', { class: 'form-grid' }, [
        el('div', { class: 'field' }, [el('label', { text: 'Theme' }), theme]),
        el('div', { class: 'field' }, [el('label', { text: 'Custom base' }), themeBase]),
        el('div', { class: 'field' }, [el('label', { text: 'Accent colour' }), themeAccent]),
        el('div', { class: 'field' }, [el('label', { text: 'Surface tint' }), themeTint]),
      ]),
      el('p', { class: 'muted', text: 'Themes are pure token swaps, so a custom accent never changes the layout.' }),
    ]),

    el('div', { class: 'card stack' }, [
      el('h2', { text: 'About' }),
      el('div', { class: 'muted', text: `Smart-Serve ${version}` }),
      el('div', { class: 'muted', text: 'S.M.A.R.T. data comes from smartmontools; the app never writes to a drive.' }),
    ]),
  );
}
