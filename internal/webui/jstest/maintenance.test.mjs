import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();
const {
  parseWindows, localInputValue, parseLocalInput, windowState, buildWindow,
  parseClock, fmtClock, describeWindow, isRecurring,
} = await import('../assets/js/views/notifications.js');

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

test('buildWindow accepts a well-formed one-off window', () => {
  const built = buildWindow({
    label: '  rebuild  ',
    recurrence: 'once',
    start: localInputValue(NOW),
    end: localInputValue(NOW + 2 * HOUR),
    serials: ['S1', 'S2'],
  });
  assert.ok(built.window, built.error);
  assert.equal(built.window.label, 'rebuild');
  assert.equal(built.window.recurrence, 'once');
  assert.equal(built.window.starts_at, NOW);
  assert.equal(built.window.ends_at, NOW + 2 * HOUR);
  assert.deepEqual(built.window.serials, ['S1', 'S2']);
});

test('buildWindow treats no picked drives as every drive', () => {
  const built = buildWindow({
    recurrence: 'once', start: localInputValue(NOW), end: localInputValue(NOW + HOUR), serials: [],
  });
  assert.deepEqual(built.window.serials, []);
});

test('buildWindow rejects a one-off window that cannot apply', () => {
  const cases = [
    [{ start: '', end: '' }, /start and end/],
    [{ start: localInputValue(NOW), end: localInputValue(NOW - HOUR) }, /after the start/],
    [{ start: localInputValue(NOW), end: localInputValue(NOW) }, /after the start/],
  ];
  for (const [values, pattern] of cases) {
    const built = buildWindow({ recurrence: 'once', ...values });
    assert.ok(built.error, `expected an error for ${JSON.stringify(values)}`);
    assert.match(built.error, pattern);
  }
});

// --- recurrence ---------------------------------------------------------------

test('parseClock reads an "HH:MM" input', () => {
  assert.equal(parseClock('00:00'), 0);
  assert.equal(parseClock('02:30'), 150);
  assert.equal(parseClock('23:59'), 1439);
  // A single-digit hour is what some browsers report for a value set in code.
  assert.equal(parseClock('9:05'), 545);
  for (const bad of ['', null, undefined, '25:00', '02:60', '2', 'nonsense', '1:2']) {
    assert.equal(parseClock(bad), null, `input ${JSON.stringify(bad)}`);
  }
});

test('fmtClock round-trips through parseClock', () => {
  for (const minutes of [0, 90, 150, 720, 1439]) {
    assert.equal(parseClock(fmtClock(minutes)), minutes);
  }
});

test('buildWindow builds a daily window', () => {
  const built = buildWindow({ recurrence: 'daily', startTime: '02:00', endTime: '04:00' });
  assert.ok(built.window, built.error);
  assert.equal(built.window.recurrence, 'daily');
  assert.equal(built.window.start_minute, 120);
  assert.equal(built.window.end_minute, 240);
  assert.deepEqual(built.window.days, []);
});

// A window that crosses midnight is legitimate, so it must not be rejected as
// "end before start".
test('buildWindow accepts a daily window across midnight', () => {
  const built = buildWindow({ recurrence: 'daily', startTime: '23:00', endTime: '02:00' });
  assert.ok(built.window, built.error);
  assert.equal(built.window.start_minute, 1380);
  assert.equal(built.window.end_minute, 120);
});

test('buildWindow builds a weekly window and sorts its days', () => {
  const built = buildWindow({
    recurrence: 'weekly', startTime: '02:00', endTime: '04:00', days: [5, 1, 1, 3],
  });
  assert.ok(built.window, built.error);
  assert.deepEqual(built.window.days, [1, 3, 5], 'days should be de-duplicated and ordered');
});

test('buildWindow rejects a recurring window that cannot apply', () => {
  assert.match(buildWindow({ recurrence: 'daily', startTime: '02:00', endTime: '02:00' }).error,
    /different from the start/);
  assert.match(buildWindow({ recurrence: 'daily', startTime: '', endTime: '04:00' }).error,
    /start and end/);
  assert.match(buildWindow({ recurrence: 'weekly', startTime: '02:00', endTime: '04:00', days: [] }).error,
    /at least one day/);
  assert.match(buildWindow({ recurrence: 'weekly', startTime: '02:00', endTime: '04:00', days: [9] }).error,
    /at least one day/);
});

// An unknown recurrence must not silently become a one-off with no dates.
test('buildWindow falls back to a one-off for an unknown recurrence', () => {
  const built = buildWindow({
    recurrence: 'fortnightly', start: localInputValue(NOW), end: localInputValue(NOW + HOUR),
  });
  assert.equal(built.window.recurrence, 'once');
});

test('describeWindow explains both kinds of window', () => {
  const daily = { recurrence: 'daily', start_minute: 120, end_minute: 240 };
  assert.equal(describeWindow(daily), 'Every day, 02:00–04:00');

  const weekly = { recurrence: 'weekly', start_minute: 1380, end_minute: 120, days: [1, 5] };
  assert.equal(describeWindow(weekly), 'Mon, Fri · 23:00–02:00');

  const once = { starts_at: NOW, ends_at: NOW + HOUR };
  assert.ok(describeWindow(once).includes('→'), describeWindow(once));
});

// Recurring windows are evaluated on the server's clock, so the browser cannot say
// whether one is live; it is labelled by its schedule instead.
test('windowState labels a recurring window as recurring', () => {
  assert.equal(windowState({ recurrence: 'daily', start_minute: 120, end_minute: 240 }, NOW), 'recurring');
  assert.equal(windowState({ recurrence: 'once', starts_at: NOW, ends_at: NOW + HOUR }, NOW), 'active');
});

test('isRecurring distinguishes the two shapes', () => {
  assert.equal(isRecurring({ recurrence: 'daily' }), true);
  assert.equal(isRecurring({ recurrence: 'weekly' }), true);
  assert.equal(isRecurring({ recurrence: 'once' }), false);
  assert.equal(isRecurring({}), false);
  assert.equal(isRecurring(null), false);
});
