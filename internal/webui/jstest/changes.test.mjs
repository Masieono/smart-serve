// The snapshot diff (FEATURES.md A18).
//
// The card answers one question, and the interesting cases are the quiet ones: "nothing changed"
// has to be a real answer rather than an empty panel, an absent reading has to look absent rather
// than like zero, and the counters the server decided were *routine* still have to be mentioned —
// a silent filter would be a lie by omission.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const { changesCard } = await import('../assets/js/views/device.js');

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

// No diff means no card at all — not an empty one, which would read as "we did not look".
test('no diff means no card', () => {
  assert.equal(changesCard(null), null);
  assert.equal(changesCard(undefined), null);
});

// The first scan has nothing to compare against, and the server says so in a note.
test('a note replaces the change list', () => {
  const text = textOf(changesCard({ note: 'Only one scan has been recorded.' }));
  assert.match(text, /Since the last scan/);
  assert.match(text, /Only one scan has been recorded/);
  assert.doesNotMatch(text, /Nothing changed/);
});

// A drive scanned twice and found identical is exactly the reassurance someone opened the page for,
// so the card says so in words rather than rendering nothing.
test('nothing changed is stated, not left blank', () => {
  const text = textOf(changesCard({ from: '2026-01-01T00:00:00Z', to: '2026-01-02T00:00:00Z' }));
  assert.match(text, /Nothing changed/);
});

test('a scalar change reads as was, then now', () => {
  const text = textOf(changesCard({
    from: '2026-01-01T00:00:00Z',
    to: '2026-01-02T00:00:00Z',
    score: { from: 96, to: 74 },
  }));
  assert.match(text, /Score/);
  assert.match(text, /96\/100/);
  assert.match(text, /74\/100/);
  assert.match(text, /→/);
  assert.doesNotMatch(text, /Nothing changed/);
});

// A drive that reported no temperature last scan has no "from". An em dash says so; a zero would
// invent a reading the drive never gave.
test('an absent end is an em dash, never a zero', () => {
  const text = textOf(changesCard({
    from: '2026-01-01T00:00:00Z',
    to: '2026-01-02T00:00:00Z',
    temperature_c: { to: 41 },
  }));
  assert.match(text, /Temperature/);
  assert.match(text, /— \| → \| 41 °C/);
});

// The routine counters are counted rather than listed, and the count is stated. Dropping them
// silently would make the card a filter nobody was told about.
test('routine movements are counted and said out loud', () => {
  const text = textOf(changesCard({
    from: '2026-01-01T00:00:00Z',
    to: '2026-01-02T00:00:00Z',
    routine: 4,
  }));
  assert.match(text, /4 other counters moved/);
  assert.match(text, /ordinary wear/);
});

test('one routine counter is singular', () => {
  const text = textOf(changesCard({
    from: '2026-01-01T00:00:00Z',
    to: '2026-01-02T00:00:00Z',
    routine: 1,
  }));
  assert.match(text, /1 other counter moved/);
});

// The counters that moved *and* are a finding get a table, because they are the reason to read the
// card at all.
test('a troubling counter gets a row of its own', () => {
  const card = changesCard({
    from: '2026-01-01T00:00:00Z',
    to: '2026-01-02T00:00:00Z',
    attributes: [{ id: 5, name: 'Reallocated_Sector_Ct', from: 0, to: 5, value: 100, threshold: 10 }],
  });
  const text = textOf(card);
  assert.match(text, /Reallocated_Sector_Ct/);
  assert.match(text, /Counter/);
  assert.equal(card.all('tbody')[0].all('tr').length, 1);
});

test('an appeared counter is marked as new', () => {
  const text = textOf(changesCard({
    attributes: [{ id: 5, name: 'Reallocated_Sector_Ct', from: 0, to: 1, appeared: true }],
  }));
  assert.match(text, /new/);
});

test('a counter at its threshold is marked as such', () => {
  const text = textOf(changesCard({
    attributes: [{ id: 187, name: 'Reported_Uncorrect', from: 0, to: 1, value: 1, threshold: 10 }],
  }));
  assert.match(text, /at threshold/);
});