// The drive's own logs (FEATURES.md A20).
//
// This panel shows counts rather than entries, and the two things worth pinning are the
// reasons it is allowed to: a count of zero must still render, because "this drive has never
// complained" is the answer that sent someone looking; and an absent count must render
// nothing at all, because an empty panel reads as "we did not look".

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const { logSummary } = await import('../assets/js/views/device.js');

/** Every string in a subtree, in document order. */
function textOf(node) {
  const out = [];
  const walk = (n) => {
    for (const child of n.childNodes) {
      if (!(child instanceof StubNode)) continue;
      if (child.tagName === '#TEXT') out.push(child.data);
      else if (child.textContent) out.push(child.textContent);
      walk(child);
    }
  };
  walk(node);
  return out.join(' | ');
}

test('a clean log renders as a finding, not as nothing', () => {
  const text = textOf(logSummary({ error_count: 0, self_test_count: 0 }));
  assert.match(text, /Error log \| none logged/);
  assert.match(text, /Self-test log \| none logged/);
});

test('counts are pluralised', () => {
  assert.match(textOf(logSummary({ error_count: 1 })), /1 entry logged/);
  assert.match(textOf(logSummary({ error_count: 3 })), /3 entries logged/);
  assert.match(textOf(logSummary({ self_test_count: 2 })), /2 results logged/);
});

// An NVMe drive that answers the self-test log request with an error reports nothing else
// about it, so this is the only place the limitation can surface.
test('an unsupported self-test log is stated, with smartctl quoted', () => {
  const text = textOf(logSummary({
    error_count: 1,
    self_test_supported: false,
    note: 'Read Self-test Log failed: Invalid Field in Command (0x4002)',
  }));
  assert.match(text, /Self-test \| not supported by this drive/);
  assert.match(text, /smartctl: Read Self-test Log failed/);
});

// The drive's own estimate of how long its tests run — the progress basis A19 needs.
test('self-test timings are shown when the drive offers them', () => {
  const text = textOf(logSummary({ short_minutes: 2, extended_minutes: 133 }));
  assert.match(text, /short ≈ 2 min/);
  assert.match(text, /extended ≈ 133 min/);
});

test('nothing reported means no panel', () => {
  assert.equal(logSummary(null), null);
  assert.equal(logSummary(undefined), null);
  assert.equal(logSummary({}), null);
});

// A drive whose only finding is a note still gets a panel: the note is the whole explanation.
test('a note alone is enough to render', () => {
  const text = textOf(logSummary({ note: 'Read Self-test Log failed' }));
  assert.match(text, /smartctl: Read Self-test Log failed/);
});

// The self-test log, rendered. It is the one worth showing: the entries carry the test, its
// outcome, and the percentage remaining for a test still running.
test('the self-test log renders its entries', () => {
  const text = textOf(logSummary({
    self_test_count: 2,
    self_test_entries: [
      { type: 'Short offline', status: 'Completed without error', passed: true, hours: 2116 },
      { type: 'Short offline', status: 'Self-test routine in progress', remaining: 50, hours: 2116 },
    ],
  }));
  assert.match(text, /Short offline/);
  assert.match(text, /Completed without error/);
  assert.match(text, /2,116 h/);
  assert.match(text, /In progress, 50% left/);
});

// An absent verdict is not a failure. A test still running reports no `passed` at all, and
// defaulting that to false would put a red cross beside a drive that is merely busy.
test('a test in progress is not rendered as a failure', () => {
  const text = textOf(logSummary({
    self_test_entries: [{ type: 'Extended offline', status: 'in progress', remaining: 12 }],
  }));
  assert.match(text, /In progress, 12% left/);
  assert.doesNotMatch(text, /Failed/);
});

test('a finished self-test shows the drive\'s own verdict', () => {
  const text = textOf(logSummary({
    self_test_entries: [
      { type: 'Short offline', status: 'Completed: read failure', passed: false, hours: 42 },
    ],
  }));
  assert.match(text, /Completed: read failure/);
});

// The error log's populated entries carry a number and an index and nothing else, so a table of
// them would be a table of dashes. The count already says it better.
test('an error log with no detail gets a sentence, not a table', () => {
  const text = textOf(logSummary({ error_count: 1, error_entries: [{ number: 1, index: 0 }] }));
  assert.match(text, /records that an error happened/);
  assert.doesNotMatch(text, /Detail/, 'no table header for an entry with nothing in it');
});

test('an error log with detail gets a table', () => {
  const text = textOf(logSummary({
    error_count: 1,
    error_entries: [{ number: 3, hours: 1200, lba: 12345678, status: 'Uncorrectable' }],
  }));
  assert.match(text, /Detail/);
  assert.match(text, /Uncorrectable/);
  assert.match(text, /1,200 h/);
});

// The self-test log records *lifetime hours*, not a date — that is all the drive keeps — and "2,116 h"
// on its own answers a question nobody asked. The history pairs each lifetime-hours reading with the
// time it was taken, so a test can be placed on a real clock; the raw figure stays beside it, because
// that is what the drive actually said.
test('a self-test is placed on a clock using the history', () => {
  const now = Date.now();
  const day = 86400000;
  const text = textOf(logSummary(
    { self_test_entries: [{ type: 'Short', status: 'Completed without error', passed: true, hours: 2116 }] },
    [
      { ts: new Date(now - 10 * day).toISOString(), value: 2000 },
      { ts: new Date(now).toISOString(), value: 2200 },
    ],
  ));
  // 2,116 is 58% of the way from 2,000 to 2,200, which leaves 42% of ten days: a little over four.
  assert.match(text, /4d ago/);
  assert.match(text, /2,116 h/);
});

// A test older than the retained history has no bracketing reading, so it says its hours rather than
// inventing a date from a rate — a figure nobody could check.
test('a test the history cannot place shows its hours', () => {
  const text = textOf(logSummary(
    { self_test_entries: [{ type: 'Short', status: 'Completed without error', hours: 100 }] },
    [
      { ts: '2026-10-01T00:00:00Z', value: 2000 },
      { ts: '2026-10-02T00:00:00Z', value: 2100 },
    ],
  ));
  assert.match(text, /100 h/);
  assert.doesNotMatch(text, /ago/, 'a date was invented for a test nothing brackets');
});

