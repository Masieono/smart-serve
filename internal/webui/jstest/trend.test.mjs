import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();
const { sparkLine, trendRow, sparkline } = await import('../assets/js/components.js');

/** The y coordinates of a path, in order. */
function pathY(path) {
  return [...path.matchAll(/[ML][\d.]+ ([\d.]+)/g)].map((m) => Number(m[1]));
}

test('sparkLine emits one point per reading', () => {
  const d = sparkLine([1, 2, 3], 40);
  assert.equal((d.match(/[ML]/g) || []).length, 3);
  assert.match(d, /^M/, 'the path must start with a moveto');
});

// A single reading is not a trend, and drawing one would imply a shape that the
// data does not support.
test('sparkLine refuses fewer than two readings', () => {
  for (const input of [[5], [], null, undefined, ['x']]) {
    assert.equal(sparkLine(input, 40), null, `sparkLine(${JSON.stringify(input)})`);
  }
});

// A flat series has no range. Dividing by zero would emit NaN coordinates and draw
// nothing at all, so it has to collapse to a straight line.
test('sparkLine renders a flat series without NaN', () => {
  const d = sparkLine([7, 7, 7], 40);
  assert.ok(d, 'a flat series should still draw');
  assert.ok(!d.includes('NaN'), `flat series produced ${d}`);
  const ys = pathY(d);
  assert.equal(new Set(ys).size, 1, `expected one y for a flat series, got ${ys}`);
});

test('sparkLine skips readings that are not numbers', () => {
  const d = sparkLine([1, null, 3, undefined, Number.NaN, 5], 40);
  assert.equal((d.match(/[ML]/g) || []).length, 3);
});

// SVG y grows downwards, so the largest reading must end up with the smallest y.
test('sparkLine puts the peak above the trough', () => {
  const ys = pathY(sparkLine([10, 20], 40));
  assert.ok(ys[1] < ys[0], `expected the second point higher up, got ${ys}`);
});

test('sparkLine keeps every point inside the box', () => {
  const ys = pathY(sparkLine([1, 50, 25, 100], 40));
  for (const y of ys) {
    assert.ok(y >= 0 && y <= 40, `y=${y} escapes the 0..40 viewBox`);
  }
});

test('trendRow drops a metric with too few readings', () => {
  const metric = { key: 'score', label: 'Health score', format: (v) => `${v}`, band: true };
  assert.equal(trendRow(metric, [{ ts: '2026-01-01T00:00:00Z', value: 90 }]), null);
});

test('trendRow reports the latest value and the change', () => {
  const metric = {
    key: 'score',
    label: 'Health score',
    format: (v) => `${v}/100`,
    band: true,
    delta: (d) => ({ text: `${d > 0 ? '+' : ''}${d}`, tone: d > 0 ? 'good' : 'bad' }),
  };
  const row = trendRow(metric, [
    { ts: '2026-01-01T00:00:00Z', value: 90 },
    { ts: '2026-01-02T00:00:00Z', value: 60 },
  ]);

  const texts = row.all('span').map((n) => n.textContent);
  assert.ok(texts.includes('60/100'), `expected the latest value, got ${texts}`);
  assert.ok(texts.includes('-30'), `expected the delta, got ${texts}`);
});

// --- the hover readout --------------------------------------------------------

const READINGS = [
  { ts: '2026-01-01T00:00:00Z', value: 90 },
  { ts: '2026-01-02T00:00:00Z', value: 80 },
  { ts: '2026-01-03T00:00:00Z', value: 60 },
];

test('sparkline refuses a series it cannot draw', () => {
  for (const input of [null, undefined, [], [{ ts: 'x', value: 1 }]]) {
    assert.equal(sparkline(input, {}), null, `input ${JSON.stringify(input)}`);
  }
});

test('the readout is hidden until the plot is hovered', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  const readout = plot.find((n) => n.className === 'spark-readout');
  assert.equal(readout.hidden, true);
  assert.equal(plot.find((n) => n.className === 'spark-marker').hidden, true);
});

// The plot is stretched to its container, so the reading comes from the pointer's
// position within the element rather than the SVG's own coordinate space.
test('hovering shows the reading under the pointer', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  plot.boundingBox = { left: 0, top: 0, right: 100, bottom: 40, width: 100, height: 40 };
  const readout = plot.find((n) => n.className === 'spark-readout');

  plot.dispatch('mousemove', { clientX: 0 });
  assert.equal(readout.hidden, false);
  assert.match(readout.textContent, /^90\/100 · /, readout.textContent);
  assert.equal(readout.dataset.edge, 'start');

  plot.dispatch('mousemove', { clientX: 50 });
  assert.match(readout.textContent, /^80\/100 · /, readout.textContent);
  assert.equal(readout.dataset.edge, 'mid');

  plot.dispatch('mousemove', { clientX: 100 });
  assert.match(readout.textContent, /^60\/100 · /, readout.textContent);
  // Anchored at the end rather than centred, so it cannot run off the card.
  assert.equal(readout.dataset.edge, 'end');

  plot.dispatch('mouseleave');
  assert.equal(readout.hidden, true);
});

test('a pointer outside the plot clamps to the nearest reading', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  plot.boundingBox = { left: 0, top: 0, right: 100, bottom: 40, width: 100, height: 40 };
  const readout = plot.find((n) => n.className === 'spark-readout');

  plot.dispatch('mousemove', { clientX: -500 });
  assert.match(readout.textContent, /^90\/100 · /, readout.textContent);

  plot.dispatch('mousemove', { clientX: 5000 });
  assert.match(readout.textContent, /^60\/100 · /, readout.textContent);
});

// A mouse-only readout would leave the figures unreachable by keyboard.
test('arrow keys step through the readings', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  const readout = plot.find((n) => n.className === 'spark-readout');

  plot.dispatch('focus');
  assert.match(readout.textContent, /^60\/100 · /, 'focus starts on the latest reading');

  plot.dispatch('keydown', { key: 'ArrowLeft' });
  assert.match(readout.textContent, /^80\/100 · /, readout.textContent);

  plot.dispatch('keydown', { key: 'ArrowLeft' });
  assert.match(readout.textContent, /^90\/100 · /, readout.textContent);

  // Stops at the first reading rather than wrapping or going undefined.
  plot.dispatch('keydown', { key: 'ArrowLeft' });
  assert.match(readout.textContent, /^90\/100 · /, readout.textContent);

  plot.dispatch('keydown', { key: 'ArrowRight' });
  assert.match(readout.textContent, /^80\/100 · /, readout.textContent);

  plot.dispatch('blur');
  assert.equal(readout.hidden, true);
});

test('the plot carries a summary for screen readers', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  assert.match(plot.getAttribute('aria-label'), /3 readings, 90\/100 to 60\/100/);
});
