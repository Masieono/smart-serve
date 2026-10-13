import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();
const { sparkLine, trendRow } = await import('../assets/js/components.js');

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
