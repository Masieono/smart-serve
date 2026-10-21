import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, stubFetch } from './stub-dom.mjs';

installStubDom();
const settings = await import('../assets/js/views/settings.js');

const ctx = { setTemperatureUnit() {}, refresh() {} };

// The knowledge base status (`FEATURES.md` A21), as GET /api/knowledge returns it. Both requests the
// settings page makes are stubbed with the same payload, so this object carries the settings map *and*
// the knowledge status — which is exactly what the page has to cope with anyway.
const KNOWLEDGE = {
  url: 'https://example.test/knowledge.json',
  bundled_entries: 12,
  resolved_entries: 14,
  cache: { entries: 13, fetched_at: '2026-10-01T00:00:00Z', source_url: 'https://example.test/k.json' },
  overrides: { entries: 1, payload: '{"version":1,"drives":[]}' },
  bundled: '{"version":1,"drives":[]}',
};

/** Every element of a tag under a class, for asserting on what the card actually says. */
function textsFor(container, tag, className) {
  return container
    .all(tag)
    .filter((node) => node.className === className)
    .map((node) => node.textContent);
}

// The knowledge card has to say where the table came from and how old it is, because the whole reason
// the refresh is a button is that the user decides when it is worth pressing — and they cannot decide
// that without seeing the age.
test('the knowledge card reports every layer', async () => {
  installStubDom();
  stubFetch({ settings: {}, ...KNOWLEDGE });

  const container = document.createElement('div');
  await render(container);

  const [status] = textsFor(container, 'div', 'muted knowledge-status');
  assert.ok(status, 'no status line');
  for (const expected of ['14 families known', '12 shipped', '13 fetched', '1 yours']) {
    assert.ok(status.includes(expected), `status "${status}" does not mention "${expected}"`);
  }

  // The overrides editor holds what is stored, so a save round-trips rather than blanking it.
  const editors = container.all('textarea').filter((n) => n.className === 'knowledge-editor');
  assert.equal(editors.length, 1);
  assert.equal(editors[0].value, KNOWLEDGE.overrides.payload);

  // The shipped table is offered as a download, which is how somebody starts their own copy to point
  // the URL at.
  const download = container
    .all('a')
    .find((n) => n.getAttribute('download') === 'smart-serve-knowledge.json');
  assert.ok(download, 'the shipped table is not offered as a download');
  assert.ok(download.getAttribute('href').startsWith('data:application/json'),
    'the download is not a data URL');
});

// Nothing stored yet is a real state, not an error: the bundled table is the floor, so the card says
// what it has and offers no "discard" for a copy that does not exist.
test('the knowledge card copes with nothing fetched and nothing typed', async () => {
  installStubDom();
  stubFetch({ settings: {}, bundled_entries: 12, resolved_entries: 12, bundled: '{"version":1}' });

  const container = document.createElement('div');
  await render(container);

  const [status] = textsFor(container, 'div', 'muted knowledge-status');
  assert.ok(status.includes('12 families known'));
  assert.ok(status.includes('nothing fetched yet'));
  assert.ok(!status.includes('yours'), 'a layer nobody has written is reported as present');

  const discard = container.all('button').find((n) => n.textContent.includes('Discard'));
  assert.ok(!discard, 'a discard button for a copy that does not exist');
});

// A copy fetched from a different URL than the one now configured is called out, because its age is
// not evidence about the URL the user is looking at.
test('a stale fetched copy is called out', async () => {
  installStubDom();
  stubFetch({ settings: {}, ...KNOWLEDGE, cache_stale: true });

  const container = document.createElement('div');
  await render(container);

  const warnings = textsFor(container, 'p', 'muted knowledge-warn');
  assert.equal(warnings.length, 1);
  assert.ok(warnings[0].includes('different URL'));
});

/** The label of every setting row on the page, in order. */
function rowLabels(container) {
  return container
    .all('div')
    .filter((node) => node.className === 'setting-label')
    .map((node) => node.textContent);
}

function render(container) {
  return settings.render(container, ctx);
}

test('the settings page renders a labelled row for every setting', async () => {
  installStubDom();
  stubFetch({ settings: {} });

  const container = document.createElement('div');
  await render(container);

  const labels = rowLabels(container);
  for (const expected of [
    'Temperature caution threshold',
    'Reallocated sectors → failing',
    'Pending sectors → failing',
    'Uncorrectable sectors → failing',
    'NVMe endurance used → caution',
    'Caution on any sector problem',
    'Alert squelch window',
    'History retention',
    'Notification log retention',
    'Temperature unit',
    'Theme',
  ]) {
    assert.ok(labels.includes(expected), `missing a row for "${expected}"`);
  }
});

test('every numeric setting gets a slider bound to its own range', async () => {
  installStubDom();
  stubFetch({ settings: {} });

  const container = document.createElement('div');
  await render(container);

  const sliders = container.all('input').filter((n) => n.className === 'setting-range');
  // Five health thresholds, three operational settings, and the two correlated-heat tunables.
  assert.equal(sliders.length, 10);

  for (const slider of sliders) {
    const min = Number(slider.getAttribute('min'));
    const max = Number(slider.getAttribute('max'));
    assert.ok(Number.isFinite(min) && Number.isFinite(max), 'slider needs numeric bounds');
    assert.ok(min < max, 'slider bounds must be the right way round');
    assert.ok(Number(slider.value) >= min && Number(slider.value) <= max,
      `default ${slider.value} is outside ${min}..${max}`);
  }
});

// The stored threshold is Celsius (what the drive reports). Showing it in
// Fahrenheit without converting would misrepresent it as a different temperature.
test('a Fahrenheit preference converts the threshold and its bounds', async () => {
  installStubDom();
  stubFetch({ settings: { temperature_unit: 'f', temperature_threshold_c: '55' } });

  const container = document.createElement('div');
  await render(container);

  const slider = container.all('input').find((n) => n.className === 'setting-range');
  assert.equal(slider.getAttribute('min'), '86'); // 30 °C
  assert.equal(slider.getAttribute('max'), '194'); // 90 °C
  assert.equal(slider.value, '131'); // 55 °C

  const unit = container
    .all('span')
    .find((n) => n.className === 'setting-unit');
  assert.equal(unit.textContent, '°F');
});

// --- the unit picker ----------------------------------------------------------

/** The Celsius/Fahrenheit picker. */
function unitPicker(container) {
  return container.all('select').find((s) => s.all('option').some((o) => o.value === 'f'));
}

function saveButton(container) {
  return container.all('button').find((b) => /save/i.test(b.textContent));
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 1));

// Switching units re-renders the page so the slider's bounds follow. A re-render
// that reads the unit back from the stored settings snaps the picker to Celsius the
// instant the user chooses Fahrenheit, which is exactly the bug this pins.
test('switching to Fahrenheit survives the re-render', async () => {
  installStubDom();
  stubFetch({ settings: { temperature_unit: 'c', temperature_threshold_c: '55' } });

  const container = document.createElement('div');
  await render(container);
  assert.equal(unitPicker(container).value, 'c');

  unitPicker(container).value = 'f';
  unitPicker(container).dispatch('change');

  assert.equal(unitPicker(container).value, 'f', 'the picker snapped back to Celsius');

  // And the threshold followed, so the slider is adjustable in the shown unit.
  const slider = container.all('input').find((n) => n.className === 'setting-range');
  assert.equal(slider.value, '131');
  assert.equal(slider.getAttribute('min'), '86');
});

// The other half of the same bug: with the picker reset to Celsius, Save persisted
// Celsius, so the choice could never be kept either.
test('saving after switching units stores Fahrenheit and keeps Celsius', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: { temperature_unit: 'c', temperature_threshold_c: '55' } }, requests);

  const container = document.createElement('div');
  await render(container);

  unitPicker(container).value = 'f';
  unitPicker(container).dispatch('change');
  saveButton(container).click();
  await flush();

  const put = requests.find((r) => r.method === 'PUT');
  assert.ok(put, 'Save should PUT the settings');
  assert.equal(put.body.temperature_unit, 'f');
  // 131 °F is the same temperature as the stored 55 °C; the database stays Celsius.
  assert.equal(put.body.temperature_threshold_c, '55');
});

// Every control on the page must reach the save payload. A control that renders but
// is never sent is a setting that silently does nothing.
test('the save payload carries every control the page renders', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: {} }, requests);

  const container = document.createElement('div');
  await render(container);

  saveButton(container).click();
  await flush();

  const put = requests.find((r) => r.method === 'PUT');
  assert.ok(put, 'Save should PUT the settings');
  for (const key of [
    'temperature_threshold_c',
    'reallocated_bad_threshold',
    'pending_bad_threshold',
    'uncorrectable_bad_threshold',
    'nvme_percentage_used_warn',
    'warn_on_any_nonzero',
    'squelch_seconds',
    'retention_days',
    'notification_log_retention_days',
    'temperature_unit',
    'theme',
    'theme_base',
    'theme_accent',
    'theme_surface_tint',
  ]) {
    assert.ok(key in put.body, `${key} is rendered but never saved`);
  }
});

