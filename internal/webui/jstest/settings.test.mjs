import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, stubFetch } from './stub-dom.mjs';

installStubDom();
const settings = await import('../assets/js/views/settings.js');

const ctx = { setTemperatureUnit() {}, refresh() {} };

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
  // Five thresholds plus three operational settings.
  assert.equal(sliders.length, 8);

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
