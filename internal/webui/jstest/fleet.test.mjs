// The dense fleet table (FEATURES.md D8).
//
// The card grid is the other view of the same list, so what is worth pinning is that this one is
// genuinely usable at fleet scale: a row per drive, the columns a fleet owner would otherwise open
// four screens to collect, and headers that actually sort — a header that looks clickable and is
// not is worse than a plain label.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const { fleetTable } = await import('../assets/js/components.js');

function drive(overrides = {}) {
  return {
    serial: 'S',
    name: 'parity',
    model: 'Samsung SSD 870 EVO',
    device_path: '/dev/sda',
    transport: 'sata',
    health: 'good',
    score: 96,
    capacity_bytes: 1000204886016,
    temperature_c: 33,
    power_on_hours: 31603,
    last_scan: '2026-01-01T00:00:00Z',
    mountpoints: [],
    smart_available: true,
    ...overrides,
  };
}

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

const table = (devices, opts) => fleetTable(devices, opts);
const bodyRows = (node) => node.all('tr').slice(1);
const headers = (node) => node.all('th');

test('one row per drive', () => {
  const rows = bodyRows(table([drive({ serial: 'a' }), drive({ serial: 'b' }), drive({ serial: 'c' })]));
  assert.equal(rows.length, 3);
});

// The columns a fleet owner would otherwise open four drives to collect. Age is the one that made
// the list endpoint grow power-on hours, so it is worth asserting on.
test('the row carries the fleet columns', () => {
  const text = textOf(bodyRows(table([drive({})]))[0]);
  for (const expected of ['parity', 'SATA', 'Samsung SSD 870 EVO', '1.0 TB', '33', '96']) {
    assert.match(text, new RegExp(expected.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  }
  const header = textOf(table([drive({})]));
  for (const label of ['Bay', 'Drive', 'Verdict', 'Score', 'Transport', 'Capacity', 'Temp', 'Age']) {
    assert.match(header, new RegExp(label));
  }
});

test('the bay is shown, and an unset one leaves the cell blank', () => {
  assert.match(textOf(bodyRows(table([drive({ bay: 'bay 4' })]))[0]), /bay 4/);
  assert.doesNotMatch(textOf(bodyRows(table([drive({})]))[0]), /bay/);
});

// A column that cannot be sorted must not look like one that can. The last column is the scan
// button, which has no value to order by at all.
test('only sortable headers advertise themselves', () => {
  const th = headers(table([drive({})], { onSort: () => {} }));
  assert.equal(th[0].getAttribute('title'), 'Sort by bay');
  assert.ok(th[2].getAttribute('title'), 'the verdict column sorts by risk');
  assert.equal(th[th.length - 1].getAttribute('title'), undefined, 'the action column does not');
  assert.equal(th[th.length - 1].getAttribute('aria-sort'), undefined);
});

test('with no onSort, no header advertises sorting at all', () => {
  for (const th of headers(table([drive({})]))) {
    assert.equal(th.getAttribute('title'), undefined);
  }
});

// The active column says which way it is sorted, and says it in the markup rather than only in a
// glyph, so a screen reader gets it too.
test('the active column is marked with its direction', () => {
  const th = headers(table([drive({})], { sortKey: 'temperature', ascending: false, onSort: () => {} }));
  const active = th.find((n) => n.getAttribute('aria-sort') === 'descending');
  assert.ok(active, 'the sorted column carries aria-sort');
  assert.match(textOf(active), /↓/);
});

test('clicking a sortable header reports its key', () => {
  const clicked = [];
  const th = headers(table([drive({})], { onSort: (key) => clicked.push(key) }));
  th[0].onclick();
  th[2].onclick();
  assert.deepEqual(clicked, ['bay', 'health']);
});

// A row opens the drive. The scan button must not also open it — the click that started a scan
// would otherwise navigate away from the page showing the result.
test('a row opens the drive, and its scan button does not', () => {
  const opened = [];
  const scanned = [];
  const row = bodyRows(table([drive({ serial: 'X' })], {
    onOpen: (s) => opened.push(s),
    onScan: (s) => scanned.push(s),
  }))[0];

  row.onclick();
  assert.deepEqual(opened, ['X']);

  const button = row.find((n) => n.tagName === 'BUTTON');
  button.onclick({ stopPropagation() {} });
  assert.deepEqual(scanned, ['X']);
  assert.deepEqual(opened, ['X'], 'the scan button did not also open the drive');
});

// A drive that was not in the last scan keeps its row, because its notes and history matter, but
// it is set aside rather than reported as a verdict.
test('a disconnected drive keeps its row and is marked', () => {
  const row = bodyRows(table([drive({ is_missing: true })]))[0];
  assert.equal(row.className, 'fleet-row-missing');
  assert.match(textOf(row), /Not connected/);
});

test('an empty table is not an error', () => {
  assert.equal(bodyRows(table([])).length, 0);
});

// Grouping (`FEATURES.md` D9). The separator spans every column so the header above stays the only
// one — a second table per group would repeat an eleven-column header once per pool.
const separators = (node) => node.all('tr').filter((r) => r.className === 'fleet-group');

test('groups add a separator row with the label and a count', () => {
  const node = table([], {
    groups: [
      { label: 'tank', devices: [drive({ serial: 'a' }), drive({ serial: 'b' })] },
      { label: 'Untagged', devices: [drive({ serial: 'c' })] },
    ],
  });
  const rows = separators(node);
  assert.equal(rows.length, 2);
  assert.match(textOf(rows[0]), /tank/);
  assert.match(textOf(rows[0]), /2 drives/);
  assert.match(textOf(rows[1]), /1 drive/, 'one drive is not "1 drives"');
});

test('a group separator spans every column', () => {
  const node = table([], { groups: [{ label: 'tank', devices: [] }] });
  const cell = separators(node)[0].all('td')[0];
  assert.equal(cell.getAttribute('colspan'), String(headers(node).length));
});

test('a flat list gets no separator', () => {
  assert.equal(separators(table([drive({})])).length, 0);
});
