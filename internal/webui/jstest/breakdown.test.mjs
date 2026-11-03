// The score breakdown (`FEATURES.md` A12).
//
// The interpreter has always produced it and the app always threw it away, so the one question a
// 0-100 score invites — "why?" — had an answer that existed for a few milliseconds. What is worth
// pinning is that the reasons *add up* to the score they claim to explain, and that a malformed
// breakdown costs the reader nothing.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const { scoreBreakdown, parsePenalties } = await import('../assets/js/views/device.js');
const { attributeTable } = await import('../assets/js/components.js');

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

const PENALTIES = [
  { reason: 'reallocated sectors (12)', points: 30, attribute_id: 5 },
  { reason: 'sectors still climbing (7 new)', points: 5 },
];

test('the breakdown states how many reasons, and what they cost together', () => {
  const text = textOf(scoreBreakdown(PENALTIES));

  assert.match(text, /2 reasons/);
  assert.match(text, /35 points off/, `the total is what makes the score add up: ${text}`);
  assert.match(text, /reallocated sectors \(12\)/);
  assert.match(text, /sectors still climbing/);
  assert.match(text, /−30/);
});

// A drive that scored cleanly has nothing to explain, and a line saying "0 reasons" is noise on
// every healthy drive in the fleet.
test('a clean drive gets no breakdown at all', () => {
  assert.equal(scoreBreakdown([]), null);
});

// One reason is not "1 reasons".
test('a single reason is singular', () => {
  assert.match(textOf(scoreBreakdown([{ reason: 'temperature (61°C)', points: 6 }])), /1 reason ·/);
});

// The stored value is a blob this page did not write, so anything unreadable has to cost the reader
// nothing rather than throwing the page away.
test('an unreadable breakdown reads as no breakdown', () => {
  for (const raw of [null, undefined, '', 'not json', '{}', '[]', '[null]', '[{"points":3}]']) {
    assert.deepEqual(parsePenalties(raw), [], `raw ${JSON.stringify(raw)}`);
  }
  assert.equal(parsePenalties(JSON.stringify(PENALTIES)).length, 2);
});

// --- the marker on the attribute row -------------------------------------------

const attr = (id, name) => ({ id, name, value: 100, worst: 100, threshold: 0, raw_value: 0 });

/** The "Counted" badges in a table, as the ids they sit on. */
function countedIds(node) {
  return node
    .all('tr')
    .filter((row) => textOf(row).includes('Counted'))
    .map((row) => row.all('td')[0].textContent);
}

// Two different claims on one row, and the table has to be able to make both: the drive's own
// threshold, and this app saying the row is part of the score. Only the ids the verdict actually
// read are marked — inferring from thresholds is what A12 existed to stop doing.
test('only the attributes the verdict read are marked as counted', () => {
  const attributes = [attr(5, 'Reallocated_Sector_Ct'), attr(9, 'Power_On_Hours'), attr(197, 'Current_Pending_Sector')];
  const blamed = new Set(PENALTIES.map((p) => p.attribute_id).filter(Boolean));

  assert.deepEqual(countedIds(attributeTable(attributes, { blamed })), ['5']);
});

test('no blamed set means no markers', () => {
  const attributes = [attr(5, 'Reallocated_Sector_Ct')];
  assert.deepEqual(countedIds(attributeTable(attributes)), []);
  assert.deepEqual(countedIds(attributeTable(attributes, { blamed: new Set() })), []);
});

// A line with no attribute id must not mark attribute 0 — or, worse, mark nothing and look broken.
test('a breakdown line with no attribute marks no row', () => {
  const attributes = [attr(0, 'unknown'), attr(5, 'Reallocated_Sector_Ct')];
  const blamed = new Set(PENALTIES.map((p) => p.attribute_id).filter(Boolean));

  assert.deepEqual(countedIds(attributeTable(attributes, { blamed })), ['5']);
});
