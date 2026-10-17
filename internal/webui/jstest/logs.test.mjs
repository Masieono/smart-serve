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
