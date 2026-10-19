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

const { visibleDevices } = await import('../assets/js/views/dashboard.js');

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
