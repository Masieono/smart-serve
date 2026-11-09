// Side-by-side drive comparison (FEATURES.md D6).
//
// The alignment itself is tested in Go (`internal/httpapi/compare_test.go`) — that is where the
// feature lives. What is pinned here is what the *browser* adds: which cell to point at, which rows
// are worth reading, and that an absent attribute renders as an absence rather than a zero.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const { worstCell, interestingRows, compareButton, driveHeading, openCompare,
  addToCompare, COMPARE_MAX_DRIVES } = await import('../assets/js/views/compare.js');

const flush = () => new Promise((resolve) => setTimeout(resolve, 1));

const cell = (value, threshold = 0) => ({ value, worst: value, threshold, raw_value: value });

// Higher is worse for a normalised value — 100 is a healthy attribute — so the *lowest* one is the
// drive to look at. Only attributes the drive publishes a threshold for have a scale at all.
test('worstCell points at the lowest value among the drives that have a threshold', () => {
  assert.equal(worstCell({ cells: [cell(100, 10), cell(60, 10), cell(90, 10)] }), 1);
  // No threshold, no scale: comparing power-on hours to pick a "worse" drive is not a judgement
  // this app makes.
  assert.equal(worstCell({ cells: [cell(1200), cell(4300)] }), -1);
  // An absent attribute is skipped rather than treated as the worst value there is.
  assert.equal(worstCell({ cells: [cell(100, 10), null, cell(80, 10)] }), 2);
  assert.equal(worstCell({ cells: [] }), -1);
  assert.equal(worstCell({}), -1);
});

test('interestingRows keeps what is flagged or differs', () => {
  const rows = [
    { id: 5, flagged: true, differs: false },
    { id: 9, flagged: false, differs: true },
    { id: 194, flagged: false, differs: false },
    { id: 197, flagged: true, differs: true },
  ];
  assert.deepEqual(interestingRows(rows).map((r) => r.id), [5, 9, 197]);
  assert.deepEqual(interestingRows([]), []);
  assert.deepEqual(interestingRows(undefined), []);
});

test('driveHeading names the drive, then what it is, then where it is', () => {
  assert.equal(driveHeading({ name: 'parity', model: '870 EVO', bay: 'bay 4' }), 'parity · 870 EVO · bay 4');
  // A drive with no name falls back to its serial, which is at least true.
  assert.equal(driveHeading({ serial: 'SN-A' }), 'SN-A');
});

// A comparison needs two drives: one is not a comparison, and none is nothing to do.
test('compareButton appears only once there is something to compare', () => {
  const ctx = {};
  assert.equal(compareButton(new Set(), ctx), null);
  assert.equal(compareButton(new Set(['a']), ctx), null);

  const button = compareButton(new Set(['a', 'b']), ctx);
  assert.ok(button, 'two drives should offer a comparison');
  // The label is a child rather than `text`, because the button carries an icon — the same shape as
  // every other icon-and-label button in the app.
  assert.equal(textOf(button), 'Compare 2');
});

// The cap is the server's (`CompareMaxDrives`); this copy is so that a seventh tick explains itself
// rather than being accepted and then refused by a round trip.
test('addToCompare refuses a seventh drive rather than dropping it silently', () => {
  const full = new Set(['a', 'b', 'c', 'd', 'e', 'f']);

  const refused = addToCompare(full, 'g', true);
  assert.equal(refused.size, COMPARE_MAX_DRIVES, 'the seventh drive should not be added');
  assert.equal(refused.has('g'), false);

  // Ticking and unticking within the cap still works, and the original set is not mutated — the
  // caller assigns the result.
  const one = addToCompare(new Set(), 'a', true);
  assert.deepEqual([...one], ['a']);
  assert.deepEqual([...addToCompare(one, 'a', false)], []);
  // Re-ticking a drive that is already in does not trip the cap.
  assert.equal(addToCompare(full, 'a', true).size, COMPARE_MAX_DRIVES);
});


// --- the dialog ----------------------------------------------------------------

/** Serve one comparison payload, and record the URL asked for. */
function stubCompare(payload, requests = null) {
  globalThis.fetch = async (url) => {
    if (requests) requests.push(String(url));
    return { ok: true, status: 200, statusText: 'OK', text: async () => JSON.stringify(payload) };
  };
}

const PAYLOAD = {
  drives: [
    { serial: 'SN-A', name: 'parity', model: 'Samsung SSD 870 EVO', health: 'good', score: 95 },
    { serial: 'SN-B', name: 'media', model: 'Samsung SSD 870 EVO', health: 'caution', score: 70 },
  ],
  rows: [
    {
      id: 5,
      name: 'Reallocated_Sector_Ct',
      differs: true,
      flagged: true,
      cells: [
        { value: 100, worst: 100, threshold: 10, raw_value: 0 },
        { value: 100, worst: 100, threshold: 10, raw_value: 12, flagged: true },
      ],
    },
    {
      id: 9,
      name: 'Power_On_Hours',
      cells: [{ value: 98, worst: 98, threshold: 0, raw_value: 1200 }, null],
    },
  ],
  nvme: [null, null],
};

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

test('the comparison renders the aligned rows, one column per drive', async () => {
  installStubDom();
  const requests = [];
  stubCompare(PAYLOAD, requests);

  openCompare({}, ['SN-A', 'SN-B']);
  await flush();

  const dialog = document.body.querySelectorAll('dialog').pop();
  assert.ok(dialog, 'the comparison should open a dialog');

  const text = textOf(dialog);
  for (const expected of ['parity', 'media', 'Reallocated_Sector_Ct', 'Power_On_Hours', '12']) {
    assert.ok(text.includes(expected), `the table is missing ${expected}: ${text}`);
  }
  assert.ok(requests[0].includes('/api/compare?serials=SN-A,SN-B'),
    `the serials should reach the API in order: ${requests[0]}`);

  // One header cell per drive, plus the attribute column.
  assert.equal(dialog.querySelectorAll('th').length, 3,
    'expected an attribute column and two drives');

  // A reader is told how much of the table is worth reading, rather than being left to scan it.
  assert.match(text, /worth reading/);
});

// An attribute only one drive has is an em dash, never a zero: an NVMe drive does not have
// "Reallocated Sector Count: 0", it does not have the attribute at all.
test('a drive without an attribute shows an absence, not a zero', async () => {
  installStubDom();
  stubCompare(PAYLOAD);

  openCompare({}, ['SN-A', 'SN-B']);
  await flush();

  const dialog = document.body.querySelectorAll('dialog').pop();
  const rows = dialog.querySelectorAll('tr').filter((r) => r.querySelectorAll('td').length);
  const hours = rows.find((r) => textOf(r).includes('Power_On_Hours'));
  assert.ok(hours, 'the power-on-hours row should be rendered');

  const cells = hours.querySelectorAll('td');
  assert.equal(cells.length, 3, 'an attribute column and one cell per drive');
  assert.equal(cells[2].textContent, '—', 'the missing cell should be an em dash');
});

// A failed comparison says so in the dialog rather than leaving an empty table, which would read as
// "these drives have nothing in common".
test('a failed comparison explains itself', async () => {
  installStubDom();
  globalThis.fetch = async () => ({
    ok: false,
    status: 500,
    statusText: 'Server Error',
    text: async () => JSON.stringify({ error: 'compare: get device' }),
  });

  openCompare({}, ['SN-A', 'SN-B']);
  await flush();

  const dialog = document.body.querySelectorAll('dialog').pop();
  assert.match(textOf(dialog), /Could not compare/, 'the dialog should say the comparison failed');
});
