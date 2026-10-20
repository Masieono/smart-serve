// Filtering and sorting the fleet (FEATURES.md D5).
//
// The rules are the feature, so they are exported pure and tested without a DOM. What is worth
// pinning is the set of decisions that would otherwise be invisible: a sort that reads as broken
// because its direction is wrong, a list that reshuffles itself between renders, and a filter that
// hides something without saying so.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();

const { visibleDevices, groupByTag, exportLinks } = await import('../assets/js/views/dashboard.js');

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

// The fleet export (FEATURES.md A23). Anchors rather than buttons, because these are files to keep
// and the browser's own save dialog is the right one to put them somewhere.
test('the fleet export is two download links, one per format', () => {
  const links = exportLinks(4);
  assert.equal(links.length, 2);
  assert.deepEqual(
    links.map((a) => a.getAttribute('href')),
    ['/api/devices/export?format=csv', '/api/devices/export?format=html'],
  );

  // The filename is part of the document: it is what the file is called in the folder it is saved
  // to, months later, beside a dozen other exports. Both are distinct, so saving both works.
  assert.deepEqual(
    links.map((a) => a.getAttribute('download')),
    ['smart-serve-fleet.csv', 'smart-serve-fleet.html'],
  );
});

// A lone drive gets the single-drive report instead — the same thing with fewer columns, and better
// for the one thing a lone drive needs.
test('a lone drive gets no fleet export', () => {
  assert.deepEqual(exportLinks(1), []);
  assert.deepEqual(exportLinks(0), []);
});
