import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, stubFetch } from './stub-dom.mjs';

installStubDom();
const settings = await import('../assets/js/views/settings.js');

// The view context the shell provides. `setNavGuard` is part of it: a settings page registers one so
// an unsaved edit asks before it is navigated away from.
const ctx = {
  setTemperatureUnit() {},
  setDisplayOptions() {},
  setNavGuard() {},
  refresh() {},
};

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
    .querySelectorAll(tag)
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
  await render(container, 'data');

  // The status lives in the section's `<summary>` now, so it is visible while the section is closed —
  // which is the point: the age of the table is what decides whether opening it is worth it.
  const [status] = textsFor(container, 'span', 'muted knowledge-status');
  assert.ok(status, 'no status line');
  for (const expected of ['14 families known', '12 shipped', '13 fetched', '1 yours']) {
    assert.ok(status.includes(expected), `status "${status}" does not mention "${expected}"`);
  }

  // The two editors live on different pages now: the knowledge overrides on Data, the per-model
  // thresholds on Thresholds. Both are still reachable, and neither page renders the other's.
  const editors = container.querySelectorAll('textarea').filter((n) => n.className === 'settings-editor');
  assert.equal(editors.length, 1, 'the Data page renders the knowledge editor and nothing else');
  const knowledgeEditor = editors.find((n) => n.getAttribute('aria-label') === 'Your knowledge base entries');
  assert.ok(knowledgeEditor, 'no knowledge editor');
  assert.equal(knowledgeEditor.value, KNOWLEDGE.overrides.payload);

  // The shipped table is offered as a download, which is how somebody starts their own copy to point
  // the URL at.
  const download = container
    .querySelectorAll('a')
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
  await render(container, 'data');

  const [status] = textsFor(container, 'span', 'muted knowledge-status');
  assert.ok(status.includes('12 families known'));
  assert.ok(status.includes('nothing fetched yet'));
  assert.ok(!status.includes('yours'), 'a layer nobody has written is reported as present');

  const discard = container.querySelectorAll('button').find((n) => n.textContent.includes('Discard'));
  assert.ok(!discard, 'a discard button for a copy that does not exist');
});

// A copy fetched from a different URL than the one now configured is called out, because its age is
// not evidence about the URL the user is looking at.
test('a stale fetched copy is called out', async () => {
  installStubDom();
  stubFetch({ settings: {}, ...KNOWLEDGE, cache_stale: true });

  const container = document.createElement('div');
  await render(container, 'data');

  const warnings = textsFor(container, 'p', 'muted knowledge-warn');
  assert.equal(warnings.length, 1);
  assert.ok(warnings[0].includes('different URL'));
});

/** The label of every setting row on the page, in order. */
function rowLabels(container) {
  return container
    .querySelectorAll('div')
    .filter((node) => node.className === 'setting-label')
    .map((node) => node.textContent);
}

function render(container, page = 'thresholds') {
  return settings.render(container, ctx, page);
}

/**
 * Every settings page, rendered.
 *
 * Settings is four pages now, so a test asking "does every setting have a row?" has to ask all four —
 * and the ones that ask about one subject render just that page.
 */
async function renderEveryPage(payload) {
  const containers = [];
  for (const id of settings.settingsPageIds()) {
    stubFetch(payload);
    const container = document.createElement('div');
    await settings.render(container, ctx, id);
    containers.push(container);
  }
  return containers;
}

test('the settings pages render a labelled row for every setting', async () => {
  installStubDom();
  const containers = await renderEveryPage({ settings: {} });

  const labels = containers.flatMap((container) => rowLabels(container));
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
  const containers = await renderEveryPage({ settings: {} });

  const sliders = containers
    .flatMap((container) => container.querySelectorAll('input'))
    .filter((n) => n.className === 'setting-range');
  // Five health thresholds, three operational settings, the two correlated-heat tunables, the
  // self-test refresh interval, the storage warning level, and the on-open refresh window.
  assert.equal(sliders.length, 13);

  for (const slider of sliders) {
    const min = Number(slider.getAttribute('min'));
    const max = Number(slider.getAttribute('max'));
    assert.ok(Number.isFinite(min) && Number.isFinite(max), 'slider needs numeric bounds');
    assert.ok(min < max, 'slider bounds must be the right way round');
    assert.ok(Number(slider.value) >= min && Number(slider.value) <= max,
      `default ${slider.value} is outside ${min}..${max}`);
  }
});

// Each page loads only what it draws. The per-model editor wants the fleet and the knowledge card
// wants the knowledge status; the other two pages should not be paying for either.
test('a page asks for only what it draws', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: {}, devices: [], bundled_entries: 1, resolved_entries: 1 }, requests);

  await render(document.createElement('div'), 'appearance');
  const appearance = requests.map((r) => r.url);
  assert.ok(!appearance.some((u) => u.includes('/api/devices')), 'Appearance fetched the fleet');
  assert.ok(!appearance.some((u) => u.includes('/api/knowledge')), 'Appearance fetched knowledge');

  requests.length = 0;
  await render(document.createElement('div'), 'data');
  const data = requests.map((r) => r.url);
  assert.ok(data.some((u) => u.includes('/api/knowledge')), 'Data needs the knowledge status');
  assert.ok(!data.some((u) => u.includes('/api/devices')), 'Data fetched the fleet');
});

// The stored threshold is Celsius (what the drive reports). Showing it in
// Fahrenheit without converting would misrepresent it as a different temperature.
test('a Fahrenheit preference converts the threshold and its bounds', async () => {
  installStubDom();
  stubFetch({ settings: { temperature_unit: 'f', temperature_threshold_c: '55' } });

  const container = document.createElement('div');
  await render(container);

  const slider = container.querySelectorAll('input').find((n) => n.className === 'setting-range');
  assert.equal(slider.getAttribute('min'), '86'); // 30 °C
  assert.equal(slider.getAttribute('max'), '194'); // 90 °C
  assert.equal(slider.value, '131'); // 55 °C

  const unit = container
    .querySelectorAll('span')
    .find((n) => n.className === 'setting-unit');
  assert.equal(unit.textContent, '°F');
});

// --- the unit picker ----------------------------------------------------------

/** The Celsius/Fahrenheit picker. */
function unitPicker(container) {
  return container.querySelectorAll('select').find((s) => s.querySelectorAll('option').some((o) => o.value === 'f'));
}

function saveButton(container) {
  return container.querySelectorAll('button').find((b) => /save/i.test(b.textContent));
}

/** The first button whose label is exactly this text. */
function buttonLabelled(container, label) {
  return container.querySelectorAll('button').find((b) => b.childNodes
    .filter((n) => n.tagName === '#TEXT')
    .map((n) => n.data)
    .join('') === label);
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 1));

// Switching units re-renders the page so the picker keeps what was chosen. A re-render that reads the
// unit back from the stored settings snaps the picker to Celsius the instant the user chooses
// Fahrenheit, which is exactly the bug this pins.
test('switching to Fahrenheit survives the re-render', async () => {
  installStubDom();
  stubFetch({ settings: { temperature_unit: 'c', temperature_threshold_c: '55' } });

  const container = document.createElement('div');
  await render(container, 'appearance');
  assert.equal(unitPicker(container).value, 'c');

  unitPicker(container).value = 'f';
  unitPicker(container).dispatch('change');

  assert.equal(unitPicker(container).value, 'f', 'the picker snapped back to Celsius');
});

// The other half of the same bug: with the picker reset to Celsius, Save persisted Celsius, so the
// choice could never be kept either.
//
// The unit is an Appearance setting now, so this is where it is saved — the temperature *threshold*
// it affects lives on the Thresholds page and is sent by that page's own Save.
test('saving after switching units stores Fahrenheit', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: { temperature_unit: 'c', temperature_threshold_c: '55' } }, requests);

  const container = document.createElement('div');
  await render(container, 'appearance');

  unitPicker(container).value = 'f';
  unitPicker(container).dispatch('change');
  saveButton(container).click();
  await flush();

  const put = requests.find((r) => r.method === 'PUT');
  assert.ok(put, 'Save should PUT the settings');
  assert.equal(put.body.temperature_unit, 'f');
  // And nothing else went with it: a page saves its own fields.
  assert.ok(!('temperature_threshold_c' in put.body),
    'Appearance saved a threshold it does not render');
});

// The threshold is stored in Celsius whatever the display unit says, so a page shown in Fahrenheit
// must send Celsius. Split across pages, the conversion is still the thing that could silently go
// wrong: 131 °F typed into the box must reach the database as 55.
test('a threshold edited in Fahrenheit is stored in Celsius', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: { temperature_unit: 'f', temperature_threshold_c: '55' } }, requests);

  const container = document.createElement('div');
  await render(container, 'thresholds');

  const box = container.querySelectorAll('input').find((n) =>
    n.className === 'setting-number'
    && n.getAttribute('aria-label') === 'Temperature caution threshold value');
  assert.equal(box.value, '131', 'the stored Celsius value was not shown in Fahrenheit');

  box.value = '140'; // 60 °C
  box.dispatch('input');
  saveButton(container).click();
  await flush();

  const put = requests.find((r) => r.method === 'PUT');
  assert.ok(put, 'Save should PUT the settings');
  assert.equal(put.body.temperature_threshold_c, '60', 'the value was stored in the display unit');
});

// Every control on every page must reach a save payload. A control that renders but is never sent is
// a setting that silently does nothing — and with four pages and per-page saves, "is every control
// covered by *some* page?" is the question worth asking rather than "is it on the one page?".
test('the save payloads together carry every control the pages render', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: {} }, requests);

  const sent = new Set();
  for (const id of settings.settingsPageIds()) {
    const container = document.createElement('div');
    await render(container, id);
    saveButton(container).click();
    await flush();
  }
  for (const put of requests.filter((r) => r.method === 'PUT')) {
    for (const key of Object.keys(put.body)) sent.add(key);
  }

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
    'show_partitions',
    'show_usage',
    'show_replace_first',
    'public_url',
    'prometheus_enabled',
    'knowledge_url',
  ]) {
    assert.ok(sent.has(key), `${key} is rendered but never saved by any page`);
  }
});

/**
 * "I don't know what reasonable thresholds are" is a real thing to not know, and the shipped values
 * are the answer to it. The button loads them into the controls rather than saving them, so a reader
 * can see what recommended *means*, compare it with what they had, and change their mind.
 */
test('Reset to recommended loads the shipped values without saving them', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: { temperature_threshold_c: '70', reallocated_bad_threshold: '3' } }, requests);

  const container = document.createElement('div');
  await render(container);

  const threshold = () => container.querySelectorAll('input').find((n) =>
    n.className === 'setting-number'
    && n.getAttribute('aria-label') === 'Temperature caution threshold value');
  assert.equal(threshold().value, '70', 'the stored value was not rendered first');

  buttonLabelled(container, 'Reset to recommended').click();
  await flush();

  assert.equal(threshold().value, '55', 'the recommended value was not loaded');
  assert.equal(requests.filter((r) => r.method === 'PUT').length, 0,
    'the reset saved without being asked');
});

/**
 * Regression: Save worked exactly once.
 *
 * `event.currentTarget` is null once dispatch ends, so the `finally` that re-enabled the button
 * threw a TypeError into an unhandled promise — silently, because nobody awaits a click handler —
 * and the button stayed disabled for the rest of the session. The second press is the assertion
 * that matters: the first one always "worked".
 *
 * Save is also disabled until something changes now, so each press here is preceded by an edit.
 * That is the same invariant seen from the other side: the button must reflect whether there is
 * anything to save, and it must never get stuck.
 */
test('Save can be pressed again after it has been pressed', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: {} }, requests);

  const container = document.createElement('div');
  await render(container);

  const save = saveButton(container);
  const slider = container.querySelectorAll('input').find((n) => n.className === 'setting-range');
  const nudge = (by) => {
    slider.value = String(Number(slider.getAttribute('min')) + by);
    slider.dispatch('input');
  };

  assert.equal(save.disabled, true, 'Save offers to save nothing on an untouched page');

  nudge(1);
  assert.equal(save.disabled, false, 'a change did not enable Save');
  save.click();
  await flush();
  assert.equal(requests.filter((r) => r.method === 'PUT').length, 1, 'the first press did not save');
  assert.equal(save.disabled, true, 'Save is still offering to save what it just saved');

  nudge(2);
  assert.equal(save.disabled, false,
    'Save stayed disabled after saving, so it can never be pressed a second time');
  save.click();
  await flush();
  assert.equal(requests.filter((r) => r.method === 'PUT').length, 2, 'the second press did not save');
});



// --- per-model thresholds (FEATURES.md A3) -------------------------------------

const { fleetModels, seedModelThresholds } = settings;

const FLEET = {
  devices: [
    { serial: 'a', model: 'Samsung SSD 870 EVO' },
    { serial: 'b', model: 'samsung ssd 870 evo' },
    { serial: 'c', model: 'WDC WD40EFRX' },
    { serial: 'd', model: '   ' },
    { serial: 'e' },
  ],
};

test('fleetModels lists each model once, in a stable order', () => {
  assert.deepEqual(fleetModels(FLEET), ['Samsung SSD 870 EVO', 'WDC WD40EFRX']);
  assert.deepEqual(fleetModels({}), []);
  assert.deepEqual(fleetModels(undefined), []);
});

const VALUES = {
  temperature_threshold_c: '55',
  reallocated_bad_threshold: '10',
  pending_bad_threshold: '10',
  uncorrectable_bad_threshold: '10',
  nvme_percentage_used_warn: '90',
  warn_on_any_nonzero: 'true',
};

test('the seed writes one entry per model at the values on screen', () => {
  assert.deepEqual(seedModelThresholds('', ['870 EVO', 'WD40EFRX'], VALUES), {
    '870 EVO': VALUES,
    WD40EFRX: VALUES,
  });
});

// The common edit is one number for one model, so the seed must not throw away what is already
// there — a button labelled "load" that discarded hand-written overrides would be one press away
// from losing them.
test('the seed keeps entries that are already there', () => {
  const existing = JSON.stringify({
    '870 EVO': { reallocated_bad_threshold: '1' },
    'Old Drive': { pending_bad_threshold: '3' },
  });
  const seeded = seedModelThresholds(existing, ['870 EVO', 'WD40EFRX'], VALUES);

  assert.deepEqual(seeded['870 EVO'], { reallocated_bad_threshold: '1' });
  assert.deepEqual(seeded['Old Drive'], { pending_bad_threshold: '3' },
    "an entry for a drive that is no longer plugged in is still the user's");
  assert.deepEqual(seeded.WD40EFRX, VALUES);
});

// A model already listed under a different capitalisation is the same model: every comparison the
// app makes about models ignores case, so seeding `samsung ssd 870 evo` beside `Samsung SSD 870 EVO`
// would be two entries for one drive.
test('the seed matches an existing entry case-insensitively', () => {
  const seeded = seedModelThresholds(
    JSON.stringify({ 'SAMSUNG SSD 870 EVO': { pending_bad_threshold: '2' } }),
    ['Samsung SSD 870 EVO'], VALUES);

  assert.deepEqual(Object.keys(seeded), ['SAMSUNG SSD 870 EVO']);
});


// The editor is part of the page's single Save, so its contents have to reach the payload — and the
// temperature has to be sent in Celsius, because that is what the parser reads. Seeding a Fahrenheit
// number would seed 130 °C.
test('the per-model editor is saved with the rest of the settings', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ settings: {}, ...KNOWLEDGE, devices: FLEET.devices }, requests);

  const container = document.createElement('div');
  await settings.render(container, ctx);

  const editor = container
    .querySelectorAll('textarea')
    .find((n) => n.getAttribute('aria-label') === 'Per-model health thresholds');
  assert.ok(editor, 'the page should carry a per-model threshold editor');

  // Seeding is the only way to get an entry without typing JSON by hand, which is the whole reason
  // the button exists.
  const seed = container.querySelectorAll('button').find((b) => b.textContent === 'Load the fleet’s models');
  assert.ok(seed, 'the page should offer the seed');
  assert.equal(seed.disabled, false, 'the fleet has models, so the seed should be usable');
  seed.click();
  assert.match(editor.value, /Samsung SSD 870 EVO/, 'the seed did not name the fleet models');

  container.querySelectorAll('button').find((b) => b.textContent === 'Save').click();
  await flush();

  const put = requests.find((r) => r.method === 'PUT');
  assert.ok(put, `the save should have been sent: ${JSON.stringify(requests)}`);
  const stored = JSON.parse(put.body.model_thresholds);
  assert.deepEqual(Object.keys(stored).sort(), ['Samsung SSD 870 EVO', 'WDC WD40EFRX']);
  assert.equal(stored['Samsung SSD 870 EVO'].temperature_threshold_c, '55',
    'the threshold should be seeded in Celsius');
});

// With nothing read yet there is nothing to seed, and a button that does nothing when pressed is
// worse than one that says so.
test('the seed is disabled when the fleet has no models', async () => {
  installStubDom();
  stubFetch({ settings: {}, ...KNOWLEDGE, devices: [] });

  const container = document.createElement('div');
  await settings.render(container, ctx);

  const seed = container.querySelectorAll('button').find((b) => b.textContent === 'Load the fleet’s models');
  assert.ok(seed, 'the seed should still be offered, so the feature is discoverable');
  assert.equal(seed.disabled, true, 'there is nothing to seed from');
});

// A rejected save shows the reason beside the box as well as in a toast: the message names a JSON
// field, and a toast is gone before it has been read.
test('a rejected per-model value is shown beside the editor', async () => {
  installStubDom();
  globalThis.fetch = async (url, init = {}) => {
    if ((init.method || 'GET') === 'PUT') {
      return {
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        text: async () => JSON.stringify({ error: 'Per-model thresholds: unknown threshold "nope"' }),
      };
    }
    return {
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify({ settings: {}, ...KNOWLEDGE, devices: [] }),
    };
  };

  const container = document.createElement('div');
  await settings.render(container, ctx);

  container.querySelectorAll('button').find((b) => b.textContent === 'Save').click();
  await flush();

  const shown = container
    .querySelectorAll('div')
    .filter((n) => n.className === 'settings-error' && !n.hidden)
    .map((n) => n.textContent);
  assert.ok(shown.some((t) => t.includes('unknown threshold')),
    `the reason should be shown beside the editor, got ${JSON.stringify(shown)}`);
});

// Nonsense in the box is not a reason to refuse to help: the seed falls back to an empty starting
// point rather than throwing at somebody mid-edit.
test('the seed survives a box that is not JSON', () => {
  assert.deepEqual(seedModelThresholds('not json', ['870 EVO'], VALUES), { '870 EVO': VALUES });
  assert.deepEqual(seedModelThresholds('[]', ['870 EVO'], VALUES), { '870 EVO': VALUES });
  assert.deepEqual(seedModelThresholds(null, [], VALUES), {});
});

