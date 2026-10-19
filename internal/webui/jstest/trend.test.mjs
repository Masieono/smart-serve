import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const { sparkLine, trendRow, sparkline, dateAxis, SPARK_WIDTH } = await import('../assets/js/components.js');
const { fmtDate, fmtTime } = await import('../assets/js/dom.js');

/** The y coordinates of a path, in order. */
function pathY(path) {
  return [...path.matchAll(/[ML][\d.]+ ([\d.]+)/g)].map((m) => Number(m[1]));
}

/** The x coordinates of a path, in order. */
function pathX(path) {
  return [...path.matchAll(/[ML]([\d.]+) [\d.]+/g)].map((m) => Number(m[1]));
}

const DAY = 86400000;
const START = Date.UTC(2026, 0, 1);

/** A reading `day` days after START, which is the shape the API sends. */
const at = (day, value) => ({ ts: new Date(START + day * DAY).toISOString(), value });

test('sparkLine emits one point per reading', () => {
  const d = sparkLine([at(0, 1), at(1, 2), at(2, 3)], 40);
  assert.equal((d.match(/[ML]/g) || []).length, 3);
  assert.match(d, /^M/, 'the path must start with a moveto');
});

// A single reading is not a trend, and drawing one would imply a shape that the
// data does not support.
test('sparkLine refuses fewer than two readings', () => {
  for (const input of [
    [at(0, 5)], [], null, undefined, ['x'],
    [{ ts: 'not a date', value: 1 }, { ts: 'also not', value: 2 }],
  ]) {
    assert.equal(sparkLine(input, 40), null, `sparkLine(${JSON.stringify(input)})`);
  }
});

// A flat series has no range. Dividing by zero would emit NaN coordinates and draw
// nothing at all, so it has to collapse to a straight line.
test('sparkLine renders a flat series without NaN', () => {
  const d = sparkLine([at(0, 7), at(1, 7), at(2, 7)], 40);
  assert.ok(d, 'a flat series should still draw');
  assert.ok(!d.includes('NaN'), `flat series produced ${d}`);
  const ys = pathY(d);
  assert.equal(new Set(ys).size, 1, `expected one y for a flat series, got ${ys}`);
});

test('sparkLine skips readings that are not numbers', () => {
  const d = sparkLine([at(0, 1), at(1, null), at(2, 3), at(3, undefined), at(4, Number.NaN), at(5, 5)], 40);
  assert.equal((d.match(/[ML]/g) || []).length, 3);
});

// SVG y grows downwards, so the largest reading must end up with the smallest y.
test('sparkLine puts the peak above the trough', () => {
  const ys = pathY(sparkLine([at(0, 10), at(1, 20)], 40));
  assert.ok(ys[1] < ys[0], `expected the second point higher up, got ${ys}`);
});

test('sparkLine keeps every point inside the box', () => {
  const ys = pathY(sparkLine([at(0, 1), at(1, 50), at(2, 25), at(3, 100)], 40));
  for (const y of ys) {
    assert.ok(y >= 0 && y <= 40, `y=${y} escapes the 0..40 viewBox`);
  }
});

// --- the x-axis is time (ADR-023) ---------------------------------------------
//
// The whole point of the rework. By index a series was always drawn edge to edge, so a 30-day
// window holding two readings looked exactly like a 30-day window holding two hundred, and a
// series that began five days ago started at the left edge as if it had always been there.

test('a short series occupies its own slice of the window', () => {
  const window = { from: START, to: START + 30 * DAY };
  const xs = pathX(sparkLine([at(0, 1), at(1, 2)], 40, window));
  assert.ok(xs[0] >= 0 && xs[0] < 1, `the first reading is at the left edge, got ${xs}`);
  // One day into a 30-day window is a thirtieth of the width, not the whole of it.
  assert.ok(xs[1] < SPARK_WIDTH / 15, `one day should be ~1/30 of the width, got ${xs[1]}`);
});

// A gap in the readings is a gap in the line, not a straight line drawn across it.
test('a gap in the readings leaves a gap in the line', () => {
  const window = { from: START, to: START + 10 * DAY };
  const xs = pathX(sparkLine([at(0, 1), at(10, 2)], 40, window));
  assert.equal(xs.length, 2);
  assert.ok(xs[1] - xs[0] > SPARK_WIDTH * 0.9, `the two ends should be far apart, got ${xs}`);
});

test('a reading outside the window is clamped into the box', () => {
  const window = { from: START + 10 * DAY, to: START + 11 * DAY };
  for (const x of pathX(sparkLine([at(0, 1), at(20, 2)], 40, window))) {
    assert.ok(x >= 0 && x <= SPARK_WIDTH, `x=${x} escapes the viewBox`);
  }
});

// With no window, a series is drawn across its own extent — which is what a standalone caller
// wants, and what keeps this testable without a card around it.
test('a series with no window fills its own extent', () => {
  const xs = pathX(sparkLine([at(0, 1), at(1, 2), at(2, 3)], 40));
  assert.equal(xs[0], 0);
  assert.equal(xs[xs.length - 1], SPARK_WIDTH);
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

// The card passes one window to every row so the rows line up. Without it each row would be
// stretched across its own extent, and two charts stacked in one card would be measuring two
// different spans.
test('trendRow draws against the window it is given', () => {
  const metric = {
    key: 'score',
    label: 'Health score',
    format: (v) => `${v}`,
    band: true,
    delta: (d) => ({ text: `${d > 0 ? '+' : ''}${d}`, tone: d > 0 ? 'good' : 'bad' }),
  };
  const window = { from: START, to: START + 30 * DAY };
  const row = trendRow(metric, [at(0, 90), at(1, 80)], { window });
  const texts = row.all('span').map((n) => n.textContent);
  assert.ok(texts.includes('80'), `expected the latest value, got ${texts}`);
  // Measured across the *series*, not the window: a drive watched for one day has one day of
  // change, and reporting 30 days of it would invent a trend.
  assert.ok(texts.includes('-10'), `expected the one-day change, got ${texts}`);
});

// --- the date axis ------------------------------------------------------------

test('the axis labels the two ends and the middle', () => {
  const axis = dateAxis({ from: START, to: START + 30 * DAY });
  const ticks = axis.all('span').map((n) => n.textContent);
  assert.equal(ticks.length, 3);
  assert.equal(ticks[0], fmtDate(new Date(START).toISOString()));
  assert.equal(ticks[2], fmtDate(new Date(START + 30 * DAY).toISOString()));
});

// An axis with no span is not an axis.
test('a window with no span produces no axis', () => {
  assert.equal(dateAxis({ from: START, to: START }), null);
  assert.equal(dateAxis({ from: START, to: START - DAY }), null);
  assert.equal(dateAxis(null), null);
  assert.equal(dateAxis({}), null);
});

// It is a caption, not content: every figure it carries is also in a hover readout.
test('the axis is hidden from screen readers', () => {
  assert.equal(dateAxis({ from: START, to: START + DAY }).getAttribute('aria-hidden'), 'true');
});

// --- the hover readout --------------------------------------------------------

const READINGS = [
  { ts: '2026-01-01T00:00:00Z', value: 90 },
  { ts: '2026-01-02T00:00:00Z', value: 80 },
  { ts: '2026-01-03T00:00:00Z', value: 60 },
];

/** A plot 100px wide, so a clientX is also its percentage across the plot. */
const BOX = { left: 0, top: 0, right: 100, bottom: 40, width: 100, height: 40 };

/**
 * The readout's three lines: value, date, time. Stacked rather than run together,
 * because one line wide enough to hold a full timestamp spans most of the card.
 */
function readoutLines(plot) {
  const readout = plot.find((n) => n.className === 'spark-readout');
  return readout.childNodes.filter((n) => n instanceof StubNode).map((n) => n.textContent);
}

/** Just the reading the pointer is over. */
function readoutValue(plot) {
  return readoutLines(plot)[0];
}

test('sparkline refuses a series it cannot draw', () => {
  for (const input of [null, undefined, [], [{ ts: 'x', value: 1 }]]) {
    assert.equal(sparkline(input, {}), null, `input ${JSON.stringify(input)}`);
  }
});

test('the readout is hidden until the plot is hovered', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  assert.equal(plot.find((n) => n.className === 'spark-readout').hidden, true);
  assert.equal(plot.find((n) => n.className === 'spark-marker').hidden, true);
});

test('the readout stacks the value, the date and the time', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  plot.boundingBox = BOX;
  plot.dispatch('mousemove', { clientX: 0 });

  const lines = readoutLines(plot);
  assert.equal(lines.length, 3, `expected three lines, got ${JSON.stringify(lines)}`);
  assert.equal(lines[0], '90/100');
  assert.equal(lines[1], fmtDate(READINGS[0].ts));
  assert.equal(lines[2], fmtTime(READINGS[0].ts));
  // If both lines carried the full timestamp the split would be pointless.
  assert.notEqual(lines[1], lines[2], 'date and time must be separate lines');
});

// The plot is stretched to its container, so the reading comes from the pointer's
// position within the element rather than the SVG's own coordinate space.
test('hovering shows the reading under the pointer', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  plot.boundingBox = BOX;
  const readout = plot.find((n) => n.className === 'spark-readout');

  plot.dispatch('mousemove', { clientX: 0 });
  assert.equal(readout.hidden, false);
  assert.equal(readoutValue(plot), '90/100');
  assert.equal(readout.dataset.edge, 'start');

  plot.dispatch('mousemove', { clientX: 50 });
  assert.equal(readoutValue(plot), '80/100');
  assert.equal(readout.dataset.edge, 'mid');

  plot.dispatch('mousemove', { clientX: 100 });
  assert.equal(readoutValue(plot), '60/100');
  // Anchored at the end rather than centred, so it cannot run off the card.
  assert.equal(readout.dataset.edge, 'end');

  plot.dispatch('mouseleave');
  assert.equal(readout.hidden, true);
});

test('a pointer outside the plot clamps to the nearest reading', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  plot.boundingBox = BOX;

  plot.dispatch('mousemove', { clientX: -500 });
  assert.equal(readoutValue(plot), '90/100');

  plot.dispatch('mousemove', { clientX: 5000 });
  assert.equal(readoutValue(plot), '60/100');
});

// A mouse-only readout would leave the figures unreachable by keyboard.
test('arrow keys step through the readings', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  const readout = plot.find((n) => n.className === 'spark-readout');

  plot.dispatch('focus');
  assert.equal(readoutValue(plot), '60/100', 'focus starts on the latest reading');

  plot.dispatch('keydown', { key: 'ArrowLeft' });
  assert.equal(readoutValue(plot), '80/100');

  plot.dispatch('keydown', { key: 'ArrowLeft' });
  assert.equal(readoutValue(plot), '90/100');

  // Stops at the first reading rather than wrapping or going undefined.
  plot.dispatch('keydown', { key: 'ArrowLeft' });
  assert.equal(readoutValue(plot), '90/100');

  plot.dispatch('keydown', { key: 'ArrowRight' });
  assert.equal(readoutValue(plot), '80/100');

  plot.dispatch('blur');
  assert.equal(readout.hidden, true);
});

test('the plot carries a summary for screen readers', () => {
  const plot = sparkline(READINGS, { format: (v) => `${v}/100` });
  assert.match(plot.getAttribute('aria-label'), /3 readings, 90\/100 to 60\/100/);
});

// The marker is positioned by time too, so it sits over the reading it names. By index, the last
// reading of a series covering only the first day of a 30-day window was marked at the right edge
// — under the wrong end of the axis, and far from the line it was pointing at.
test('the hover marker sits where the reading is, not at the edge', () => {
  const window = { from: START, to: START + 30 * DAY };
  const plot = sparkline([at(0, 90), at(1, 80)], { window, format: (v) => `${v}` });
  plot.boundingBox = BOX;
  plot.dispatch('mousemove', { clientX: 100 });

  const marker = plot.find((n) => n.className === 'spark-marker');
  const left = Number.parseFloat(marker.style.left);
  assert.ok(left < 10, `the marker should sit near the left of the window, got ${left}%`);
  assert.equal(readoutValue(plot), '80', 'and it still names the reading nearest the pointer');
});
