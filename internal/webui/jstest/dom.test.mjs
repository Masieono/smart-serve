import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const {
  el, mount, fmtBytes, fmtHours, fmtInt, scoreGrade,
  fmtTemp, setTemperatureUnit, temperatureUnitName, convertTemp, toCelsius, convertTempDelta,
} = await import('../assets/js/dom.js');

// Regression: an array of nodes used to be stringified into
// "[object HTMLElement],[object HTMLElement]" instead of being appended.
test('mount flattens an array of nodes instead of stringifying them', () => {
  const grid = document.createElement('div');
  mount(grid, [el('article'), el('article'), el('article')]);
  assert.equal(grid.childNodes.length, 3);
  assert.ok(grid.childNodes.every((node) => node instanceof StubNode));
});

test('mount accepts a mix of nodes, arrays and text', () => {
  const host = document.createElement('div');
  mount(host, el('h1'), [el('p'), el('p')], 'trailing text');
  assert.equal(host.childNodes.length, 4);
});

test('el appends nested arrays of children', () => {
  const row = el('div', {}, [el('span'), [el('span'), el('span')]]);
  assert.equal(row.childNodes.length, 3);
});

test('table-style row arrays render as rows', () => {
  const tbody = el('tbody', {}, [
    el('tr', {}, [el('td', { text: 'a' })]),
    el('tr', {}, [el('td', { text: 'b' })]),
  ]);
  assert.equal(tbody.childNodes.length, 2);
});

test('formatters', () => {
  assert.equal(fmtBytes(1000204886016), '1.0 TB');
  assert.equal(fmtBytes(null), 'unknown');
  assert.equal(fmtHours(8760), '1.0 yr');
  assert.equal(fmtHours(48), '2 d');
  assert.equal(scoreGrade(100), 'Excellent');
  assert.equal(scoreGrade(74), 'Fair');
  assert.equal(scoreGrade(10), 'Critical');
  assert.equal(scoreGrade(-1), 'n/a');
});

test('fmtInt groups raw counters and blanks missing values', () => {
  assert.equal(fmtInt(null), '—');
  assert.equal(fmtInt(undefined), '—');
  assert.equal(fmtInt(0), '0');
  // Locale-aware grouping; compare against the platform's own rendering.
  assert.equal(fmtInt(31603), (31603).toLocaleString());
});

// Celsius is canonical everywhere else — API, database, notification payloads — so
// the unit must be a display-only concern that cannot leak into stored values.
test('temperature display honours the configured unit', () => {
  setTemperatureUnit('c');
  assert.equal(temperatureUnitName(), 'C');
  assert.equal(fmtTemp(33), '33 °C');
  assert.equal(convertTemp(33), 33);

  setTemperatureUnit('f');
  assert.equal(temperatureUnitName(), 'F');
  assert.equal(fmtTemp(33), '91 °F'); // 33 °C = 91.4 °F
  assert.equal(convertTemp(0), 32);
  assert.equal(convertTemp(100), 212);
  assert.equal(toCelsius(32), 0);
  assert.equal(toCelsius(212), 100);

  setTemperatureUnit('c');
});

// A settings slider converts on every drag, so a value must survive a round trip
// through the display unit unchanged or the number would creep as it is moved.
test('temperature round-trips through Fahrenheit without drifting', () => {
  for (const celsius of [30, 45, 55, 70, 90]) {
    assert.equal(toCelsius(convertTemp(celsius, 'f'), 'f'), celsius);
  }
});

test('an unrecognised temperature unit falls back to Celsius', () => {
  setTemperatureUnit('kelvin');
  assert.equal(temperatureUnitName(), 'C');
  assert.equal(fmtTemp(20), '20 °C');
  setTemperatureUnit('c');
});

test('fmtTemp blanks missing readings', () => {
  assert.equal(fmtTemp(null), '—');
  assert.equal(fmtTemp(undefined), '—');
});

// A delta is a span, not a reading: converting it with the offset would report a
// 2 °C rise as a 36 °F rise.
test('a temperature delta converts without the offset', () => {
  assert.equal(convertTempDelta(2, 'c'), 2);
  assert.equal(convertTempDelta(2, 'f'), 4); // 2 °C = 3.6 °F, rounded
  assert.equal(convertTempDelta(-5, 'f'), -9); // -5 °C = -9 °F
  assert.equal(convertTempDelta(null, 'f'), null);
  assert.equal(convertTempDelta(undefined, 'f'), null);
});
