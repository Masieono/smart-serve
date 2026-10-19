// The replacement ranking (FEATURES.md A24).
//
// The card answers "which one do I buy this month?", which is not the question the dashboard's
// existing sort answers. What is worth pinning is that it stays quiet when there is nothing to say,
// that it shows its reasons, and that it is a shortlist rather than a list.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const { replaceFirstCard } = await import('../assets/js/views/dashboard.js');

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

function stubCtx() {
  return { opened: [], openDevice(serial) { this.opened.push(serial); } };
}

const rank = (serial, band, reasons = []) => ({ serial, name: serial, band, reasons });

// A ranking of healthy drives is a ranking of nothing, and a section that is always present stops
// being read — the same reasoning as the disconnected toggle and the sort row.
test('nothing worth replacing means no card', () => {
  assert.equal(replaceFirstCard(null, stubCtx()), null);
  assert.equal(replaceFirstCard([], stubCtx()), null);
  assert.equal(replaceFirstCard([rank('a', 'fine')], stubCtx()), null);
});

test('a ranked drive shows its band and its reasons', () => {
  const text = textOf(replaceFirstCard([rank('a', 'now', ['failing now'])], stubCtx()));
  assert.match(text, /Replace first/);
  assert.match(text, /Replace now/);
  assert.match(text, /failing now/);
});

test('every band is named in words rather than shown as a code', () => {
  const cases = [['now', 'Replace now'], ['soon', 'Replace soon'], ['watch', 'Watch']];
  for (const [band, label] of cases) {
    assert.match(textOf(replaceFirstCard([rank('a', band)], stubCtx())), new RegExp(label));
  }
});

// A shortlist, not a list: past a handful, the answer to "which one first?" stops being an answer.
test('the list is capped and says how many are left', () => {
  const many = Array.from({ length: 8 }, (_, i) => rank(`s${i}`, 'watch'));
  const card = replaceFirstCard(many, stubCtx());
  assert.equal(card.all('li').length, 5);
  assert.match(textOf(card), /3 more drives/);
});

test('one leftover is singular', () => {
  const six = Array.from({ length: 6 }, (_, i) => rank(`s${i}`, 'watch'));
  assert.match(textOf(replaceFirstCard(six, stubCtx())), /1 more drive has something against it/);
});

test('the drive name opens it', () => {
  const ctx = stubCtx();
  const card = replaceFirstCard([rank('S1', 'now')], ctx);
  card.find((n) => n.tagName === 'BUTTON').onclick();
  assert.deepEqual(ctx.opened, ['S1']);
});

// An unnamed drive is identified by its serial rather than left blank.
test('a drive with no name falls back to its serial', () => {
  const card = replaceFirstCard([{ serial: 'SN-123', band: 'now', reasons: [] }], stubCtx());
  assert.match(textOf(card), /SN-123/);
});

// A drive with no reasons still renders: the band is the answer, and the reasons are the detail.
test('a drive with no reasons still appears', () => {
  const card = replaceFirstCard([{ serial: 'S1', band: 'now', reasons: [] }], stubCtx());
  assert.equal(card.all('li').length, 1);
  assert.match(textOf(card), /Replace now/);
});