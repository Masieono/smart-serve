// Filtering and sorting the fleet (FEATURES.md D5).
//
// The rules are the feature, so they are exported pure and tested without a DOM. What is worth
// pinning is the set of decisions that would otherwise be invisible: a sort that reads as broken
// because its direction is wrong, a list that reshuffles itself between renders, and a filter that
// hides something without saying so.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode, stubFetch } from './stub-dom.mjs';

installStubDom();

const { visibleDevices, groupByTag, exportButton, fleetTags, benchSession, heatCard, render, setView } =
  await import('../assets/js/views/dashboard.js');

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

/** A device payload carrying the fields the sort and filter rules read. */
function drive(overrides = {}) {
  return {
    serial: 'S',
    name: '',
    model: 'Model',
    transport: 'sata',
    health: 'good',
    capacity_bytes: 1000,
    temperature_c: 30,
    last_scan: '2026-01-01T00:00:00Z',
    mountpoints: [],
    ...overrides,
  };
}

const serials = (list) => list.map((d) => d.serial);

// The default view, and the one that matters most: a fleet owner opening the page wants the drive
// that is about to fail, not the one that happens to sort first alphabetically.
test('risk puts the worst drive first', () => {
  const fleet = [
    drive({ serial: 'good' }),
    drive({ serial: 'unknown', health: 'unknown' }),
    drive({ serial: 'bad', health: 'bad' }),
    drive({ serial: 'caution', health: 'caution' }),
  ];
  assert.deepEqual(serials(visibleDevices(fleet)), ['bad', 'caution', 'unknown', 'good']);
});

// A rack of identical drives must not shuffle between renders. The tiebreak is what stops it, and
// a list that reorders itself when nothing changed makes you re-read it every time.
test('equal ranks fall back to name, so the order is stable', () => {
  const fleet = [
    drive({ serial: 'c', name: 'charlie' }),
    drive({ serial: 'a', name: 'alpha' }),
    drive({ serial: 'b', name: 'bravo' }),
  ];
  assert.deepEqual(serials(visibleDevices(fleet)), ['a', 'b', 'c']);
});

// "First" means different things per field. Sorting by capacity wants the biggest at the top;
// getting that backwards makes the control feel broken on first use.
test('capacity and temperature default to largest and hottest first', () => {
  const fleet = [
    drive({ serial: 'small', capacity_bytes: 100, temperature_c: 20 }),
    drive({ serial: 'big', capacity_bytes: 900, temperature_c: 50 }),
  ];
  assert.deepEqual(serials(visibleDevices(fleet, { sortKey: 'capacity' })), ['big', 'small']);
  assert.deepEqual(serials(visibleDevices(fleet, { sortKey: 'temperature' })), ['big', 'small']);
});

test('reversing a sort is honoured', () => {
  const fleet = [drive({ serial: 'a', name: 'alpha' }), drive({ serial: 'b', name: 'bravo' })];
  assert.deepEqual(
    serials(visibleDevices(fleet, { sortKey: 'name', ascending: false })),
    ['b', 'a'],
  );
});

// A drive that reports no temperature is not the coolest drive in the fleet. Treating an absent
// reading as zero would put it first and make it look like the healthiest thing on the page.
test('a drive with no reading sorts last, not first', () => {
  const fleet = [
    drive({ serial: 'none', temperature_c: null }),
    drive({ serial: 'cool', temperature_c: 22 }),
    drive({ serial: 'hot', temperature_c: 48 }),
  ];
  assert.deepEqual(
    serials(visibleDevices(fleet, { sortKey: 'temperature' })),
    ['hot', 'cool', 'none'],
  );
});

test('a drive with no capacity sorts last too', () => {
  const fleet = [
    drive({ serial: 'none', capacity_bytes: null }),
    drive({ serial: 'big', capacity_bytes: 900 }),
  ];
  assert.deepEqual(serials(visibleDevices(fleet, { sortKey: 'capacity' })), ['big', 'none']);
});

// The disconnected toggle is what reveals them, and it must not be able to flatter the health
// counts on the way — which is why the counts come from a separate list.
test('a disconnected drive is hidden until asked for', () => {
  const fleet = [
    drive({ serial: 'here', name: 'bravo' }),
    drive({ serial: 'gone', name: 'alpha', is_missing: true }),
  ];
  assert.deepEqual(serials(visibleDevices(fleet)), ['here']);
  assert.deepEqual(serials(visibleDevices(fleet, { showMissing: true })), ['gone', 'here']);
});

// The filter matches the value the server sent, not the label the UI draws. Filtering on "NVMe"
// would match nothing, because the field is "nvme".
test('the transport filter matches the stored value', () => {
  const fleet = [
    drive({ serial: 'nvme', transport: 'nvme' }),
    drive({ serial: 'usb', transport: 'usb' }),
    drive({ serial: 'sata', transport: 'sata' }),
  ];
  assert.deepEqual(serials(visibleDevices(fleet, { transport: 'nvme' })), ['nvme']);
  assert.deepEqual(serials(visibleDevices(fleet, { transport: 'NVMe' })), []);
  assert.equal(visibleDevices(fleet, { transport: '' }).length, 3);
});

// Hunting a drive by where it physically is is the whole point of recording a bay, so the search
// has to cover it.
test('search covers the bay', () => {
  const fleet = [
    drive({ serial: 'a', name: 'parity', bay: 'bay 4' }),
    drive({ serial: 'b', name: 'cache', bay: 'bay 9' }),
  ];
  assert.deepEqual(serials(visibleDevices(fleet, { query: 'bay 9' })), ['b']);
});

test('search covers name, model, serial and mountpoint', () => {
  const fleet = [
    drive({ serial: 'SN-A', name: 'parity', model: 'Samsung 870' }),
    drive({ serial: 'SN-B', name: 'cache', model: 'WD Red', mountpoints: ['/mnt/tank'] }),
  ];
  for (const needle of ['parity', 'samsung', 'sn-b', 'tank']) {
    assert.equal(visibleDevices(fleet, { query: needle }).length, 1, `searching ${needle}`);
  }
});

// Filters are a conjunction. A health chip plus a transport plus a search has to mean all three,
// or the counts in the header would stop describing what is on screen.
test('filters combine', () => {
  const fleet = [
    drive({ serial: 'hit', health: 'caution', transport: 'usb', name: 'spare' }),
    drive({ serial: 'wrong-health', health: 'good', transport: 'usb', name: 'spare' }),
    drive({ serial: 'wrong-transport', health: 'caution', transport: 'sata', name: 'spare' }),
    drive({ serial: 'wrong-name', health: 'caution', transport: 'usb', name: 'other' }),
  ];
  const got = visibleDevices(fleet, { health: 'caution', transport: 'usb', query: 'spar' });
  assert.deepEqual(serials(got), ['hit']);
});

// Sorting must not mutate what it was given: the same array is what the health counts are built
// from, and re-sorting it in place would reorder the header's numbers.
test('sorting leaves the input alone', () => {
  const fleet = [drive({ serial: 'b', name: 'bravo' }), drive({ serial: 'a', name: 'alpha' })];
  visibleDevices(fleet, { sortKey: 'name' });
  assert.deepEqual(serials(fleet), ['b', 'a']);
});

test('an empty fleet is not an error', () => {
  assert.deepEqual(visibleDevices([]), []);
});

// Grouping by tag (FEATURES.md D9).
//
// The judgement worth pinning is that a drive with two tags appears under both. First-tag-wins
// would be tidier — the group counts would add up — but it silently drops a drive from a group it
// is tagged for, and the whole reason to group a fleet is not to miss one.
const labels = (groups) => groups.map((g) => g.label);
const inGroup = (groups, label) => groups.find((g) => g.label === label).devices.map((d) => d.serial);

test('a drive lands in the group for its tag', () => {
  const groups = groupByTag([
    drive({ serial: 'a', tags: 'tank' }),
    drive({ serial: 'b', tags: 'backup' }),
  ]);
  assert.deepEqual(labels(groups), ['backup', 'tank']);
  assert.deepEqual(inGroup(groups, 'tank'), ['a']);
});

// The decision, stated as a test.
test('a drive with two tags appears under both', () => {
  const groups = groupByTag([drive({ serial: 'a', tags: 'tank,critical' })]);
  assert.deepEqual(labels(groups), ['critical', 'tank']);
  assert.deepEqual(inGroup(groups, 'tank'), ['a']);
  assert.deepEqual(inGroup(groups, 'critical'), ['a']);
});

// Untagged drives are the ones with something left to do to them, so they come last rather than
// sorting among the pools by the letter U.
test('untagged drives come last', () => {
  const groups = groupByTag([
    drive({ serial: 'z', tags: 'zpool' }),
    drive({ serial: 'none' }),
    drive({ serial: 'a', tags: 'apool' }),
  ]);
  assert.deepEqual(labels(groups), ['apool', 'zpool', 'Untagged']);
  assert.deepEqual(inGroup(groups, 'Untagged'), ['none']);
});

test('tags are trimmed, and a blank tag is not a group', () => {
  const groups = groupByTag([drive({ serial: 'a', tags: ' tank , , backup ' })]);
  assert.deepEqual(labels(groups), ['backup', 'tank']);
});

// An all-untagged fleet is one group, and the function says so. The dashboard is what declines to
// offer the toggle in that case, because a single "Untagged" heading is the list with a heading on
// it rather than a grouping.
test('an all-untagged fleet is one group', () => {
  assert.deepEqual(labels(groupByTag([drive({ serial: 'a' })])), ['Untagged']);
  assert.deepEqual(groupByTag([]), []);
});

test('grouping keeps the order the list arrived in', () => {
  // Sorting is D5's job and grouping must not undo it: within a group the risk order survives.
  const sorted = visibleDevices([
    drive({ serial: 'good', tags: 'tank' }),
    drive({ serial: 'bad', health: 'bad', tags: 'tank' }),
  ]);
  assert.deepEqual(inGroup(groupByTag(sorted), 'tank'), ['bad', 'good']);
});

// The fleet export (FEATURES.md A23). One button rather than two links: the format and the scope are
// a single decision made once, and two anchors at the end of a controls row read as two more ways to
// look at the page rather than as a file to keep.
test('the fleet export is one button, not two links', () => {
  const button = exportButton([drive({ serial: 'a' }), drive({ serial: 'b' })], []);
  assert.ok(button, 'a fleet gets an export control');
  assert.equal(button.tagName, 'BUTTON');
  assert.match(textOf(button), /Export/);
});

// A lone drive gets the single-drive report instead — the same thing with fewer columns, and better
// for the one thing a lone drive needs.
test('a lone drive gets no fleet export', () => {
  assert.equal(exportButton([drive({ serial: 'a' })], []), null);
  assert.equal(exportButton([], []), null);
});

// The tags the export can be scoped to, listed once each and in a stable order so the dialog's
// dropdown does not reshuffle between renders.
test('fleetTags lists every tag once', () => {
  const fleet = [
    drive({ serial: 'a', tags: 'tank, spare' }),
    drive({ serial: 'b', tags: 'spare,bench' }),
    drive({ serial: 'c', tags: '   ' }),
  ];
  assert.deepEqual(fleetTags(fleet), ['bench', 'spare', 'tank']);
});

// The bench filter (`FEATURES.md` D11) reads the server's `on_bench` flag rather than the tag string,
// so the UI never has to know the tag's name or split a tag list the way the grouping does.
test('the bench filter uses the server flag, not the tag string', () => {
  const all = [
    drive({ serial: 'in-service' }),
    drive({ serial: 'on-bench', on_bench: true, tags: 'Bench, spare' }),
  ];

  assert.deepEqual(visibleDevices(all, { benchOnly: true }).map((d) => d.serial), ['on-bench']);
  assert.deepEqual(visibleDevices(all).map((d) => d.serial).sort(), ['in-service', 'on-bench']);
});

// The testing session card (`FEATURES.md` D11).
//
// Absent when the bench is empty — a panel that is always present stops being read — and when present
// it names every drive in the batch rather than a shortlist: the session *is* the batch, so
// truncating it would be answering a different question.
test('the testing session names the whole batch and offers its verbs', () => {
  assert.equal(benchSession([], {}, 'bench'), null);

  const bench = [
    drive({ serial: 'b1', name: 'spare-1', on_bench: true, tags: 'bench' }),
    drive({ serial: 'b2', name: 'spare-2', on_bench: true, tags: 'bench' }),
  ];
  const card = benchSession(bench, { openDevice: () => {} }, 'bench');

  const text = textOf(card);
  assert.match(text, /Testing session/);
  assert.match(text, /2 drives/);
  for (const name of ['spare-1', 'spare-2']) assert.ok(text.includes(name), `missing ${name}`);

  // Named as chips rather than as one row each: every drive is still there, in a fraction of the
  // card. The tag name is deliberately not spelled out — it is the mechanism, and what a reader
  // needs is what the state *means*.
  const chips = card.all('button').filter((b) => b.className.includes('chip-link'));
  assert.equal(chips.length, 2, 'the batch is not listed as chips');
  assert.ok(!/tagged bench/.test(text), 'the card explains the tag rather than the state');

  // The batch's own sheet, scoped by tag: it cannot be the whole fleet's file under another name.
  const link = card.all('a')[0];
  assert.equal(link.getAttribute('href'), '/api/devices/export?format=csv&tag=bench');
  assert.equal(link.getAttribute('download'), 'smart-serve-fleet-bench.csv');

  // Every verb the batch needs is reachable from the card.
  for (const label of ['Test all', 'Export batch', 'Forget all']) {
    assert.ok(text.includes(label), `missing ${label}`);
  }
});

// The enclosure heat card (`FEATURES.md` A25).
//
// It reports a correlation and never a cause — a RAID rebuild heats everything too, and so does a warm
// afternoon — and it shows the ratio rather than a bare count, because six of six is a different
// finding from six of twenty-four.
test('the heat card reports the group and its ratio, not a diagnosis', () => {
  assert.equal(heatCard([], {}), null);
  assert.equal(heatCard(undefined, {}), null);

  const card = heatCard([
    { group: 'tank', drives: ['a', 'b', 'c'], total: 4, delta_c: 9, max_c: 47 },
  ], { openDevice: () => {} });

  const text = textOf(card);
  assert.match(text, /Heating together/);
  assert.match(text, /\+9 °C/);
  assert.match(text, /3 of 4 drives/);
  assert.match(text, /47 °C/);
  // The words that keep it honest.
  assert.match(text, /correlation, not a diagnosis/);
  for (const word of ['overheating', 'failed', 'dying']) {
    assert.ok(!text.toLowerCase().includes(word), `claims a diagnosis: ${word}`);
  }

  // Every drive named is reachable, because the next step is always to go and look at one.
  assert.equal(card.all('button').length, 3);
});

// Two enclosures heating at once are two findings, not one.
test('the heat card shows every group', () => {
  const card = heatCard([
    { group: 'tank', drives: ['a', 'b', 'c'], total: 3, delta_c: 9 },
    { group: 'media', drives: ['d', 'e', 'f'], total: 6, delta_c: 12 },
  ], { openDevice: () => {} });

  assert.equal(card.all('button').length, 6);
  assert.match(textOf(card), /media/);
});

// --- the cards/table switch (docs/UI.md §7.1) ----------------------------------
//
// The view is a *preference* rather than view state, which is why it is stored server-side like the
// theme and the temperature unit: a reader who wants the table wants the table after a refresh, not
// cards again. The filter and the sort beside it stay module-only, and that difference is deliberate.

/** The first button whose label is exactly this text. */
function buttonLabelled(node, label) {
  return node.all('button').find((b) => b.childNodes
    .filter((n) => n.tagName === '#TEXT')
    .map((n) => n.data)
    .join('') === label);
}

const listCtx = { openDevice() {}, scanDevice() {}, refresh() {} };
const flush = () => new Promise((resolve) => setTimeout(resolve, 1));

test('the list opens in the stored view', async () => {
  installStubDom();
  stubFetch({ devices: [drive({ serial: 'a' }), drive({ serial: 'b' })] });

  try {
    setView('table');
    const container = document.createElement('div');
    await render(container, listCtx);
    assert.ok(container.all('table').length, 'the stored table view was not drawn');
  } finally {
    // The module keeps the choice, so leaving it set would decide the next test's first render.
    setView('cards');
  }
});

test('choosing a view stores it, so it survives a refresh', async () => {
  installStubDom();
  const requests = [];
  stubFetch({ devices: [drive({ serial: 'a' }), drive({ serial: 'b' })] }, requests);

  try {
    setView('cards');
    const container = document.createElement('div');
    await render(container, listCtx);
    assert.equal(container.all('table').length, 0, 'cards were expected to be the default');

    const table = buttonLabelled(container, 'Table');
    assert.ok(table, 'the table half of the switch is not rendered');
    table.click();
    await flush();

    const put = requests.find((r) => r.method === 'PUT');
    assert.ok(put, 'choosing a view should store it');
    assert.equal(put.body.dashboard_view, 'table');
    assert.ok(container.all('table').length, 'the table was not drawn after the switch');
  } finally {
    setView('cards');
  }
});
