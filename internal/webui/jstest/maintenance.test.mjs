import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();
const { parseWindows, localInputValue, parseLocalInput, windowState, buildWindow } =
  await import('../assets/js/views/notifications.js');

const HOUR = 3600000;
const NOW = Date.UTC(2026, 9, 12, 12, 0, 0);

function windowJSON(overrides = {}) {
  return JSON.stringify([{ label: 'rebuild', starts_at: NOW, ends_at: NOW + 2 * HOUR, serials: [], ...overrides }]);
}

// --- parsing ------------------------------------------------------------------

test('parseWindows reads a stored window', () => {
  const windows = parseWindows(windowJSON());
  assert.equal(windows.length, 1);
  assert.equal(windows[0].label, 'rebuild');
});

// A malformed setting must look like "no windows", matching the server. Anything
// else would show a silence that is not in effect.
test('parseWindows treats anything malformed as no windows', () => {
  for (const raw of ['', '   ', null, undefined, 'not json', '{}', '"a string"', 'null', '[]']) {
    assert.deepEqual(parseWindows(raw), [], `input ${JSON.stringify(raw)}`);
  }
});

test('parseWindows drops windows that could never apply', () => {
  const raw = JSON.stringify([
    { label: 'backwards', starts_at: NOW + HOUR, ends_at: NOW },
    { label: 'no end', starts_at: NOW, ends_at: NOW },
    { label: 'missing times' },
    { label: 'good', starts_at: NOW, ends_at: NOW + HOUR },
  ]);
  assert.deepEqual(parseWindows(raw).map((w) => w.label), ['good']);
});

// --- form values --------------------------------------------------------------

// datetime-local has no timezone suffix and is interpreted as local time; writing a
// UTC value into it would silently shift the window by the browser's offset.
test('localInputValue and parseLocalInput round-trip', () => {
  const ms = new Date(2026, 9, 12, 3, 30, 0).getTime();
  const value = localInputValue(ms);
  assert.match(value, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/);
  assert.equal(parseLocalInput(value), ms);
});

test('parseLocalInput rejects an empty or nonsense value', () => {
  for (const value of ['', null, undefined, 'not a date']) {
    assert.equal(parseLocalInput(value), null, `input ${JSON.stringify(value)}`);
  }
});

test('windowState reports where a window sits relative to now', () => {
  const w = { starts_at: NOW, ends_at: NOW + HOUR };
  assert.equal(windowState(w, NOW - 1), 'upcoming');
  assert.equal(windowState(w, NOW), 'active');
  assert.equal(windowState(w, NOW + HOUR - 1), 'active');
  // The end is exclusive, matching the server.
  assert.equal(windowState(w, NOW + HOUR), 'ended');
});

test('buildWindow accepts a well-formed window', () => {
  const built = buildWindow({
    label: '  rebuild  ',
    start: localInputValue(NOW),
    end: localInputValue(NOW + 2 * HOUR),
    serials: ' S1 , S2 ,, ',
  });
  assert.ok(built.window, built.error);
  assert.equal(built.window.label, 'rebuild');
  assert.equal(built.window.starts_at, NOW);
  assert.equal(built.window.ends_at, NOW + 2 * HOUR);
  assert.deepEqual(built.window.serials, ['S1', 'S2'], 'blank entries should be dropped');
});

test('buildWindow treats blank serials as every drive', () => {
  const built = buildWindow({
    start: localInputValue(NOW), end: localInputValue(NOW + HOUR), serials: '   ',
  });
  assert.deepEqual(built.window.serials, []);
});

test('buildWindow rejects a window that cannot apply', () => {
  const cases = [
    [{ start: '', end: '' }, /start and end/],
    [{ start: localInputValue(NOW), end: localInputValue(NOW - HOUR) }, /after the start/],
    [{ start: localInputValue(NOW), end: localInputValue(NOW) }, /after the start/],
  ];
  for (const [values, pattern] of cases) {
    const built = buildWindow(values);
    assert.ok(built.error, `expected an error for ${JSON.stringify(values)}`);
    assert.match(built.error, pattern);
  }
});
