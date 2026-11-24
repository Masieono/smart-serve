import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, stubFetch, StubNode } from './stub-dom.mjs';

installStubDom();
const fleet = await import('../assets/js/views/fleet.js');

/** A drive as `/api/devices` sends it, with only what these tests care about spelled out. */
function drive(over) {
  return {
    serial: 'S1', name: 'sda', model: 'Some SSD', health: 'good', score: 95,
    is_missing: false, hidden: false, temperature_c: 30, power_on_hours: 1000, used_pct: 40,
    ...over,
  };
}

const FLEET = [
  drive({ serial: 'A', name: 'alpha', health: 'good', score: 96, temperature_c: 33, power_on_hours: 900, used_pct: 20 }),
  drive({ serial: 'B', name: 'bravo', health: 'bad', score: 12, temperature_c: null, power_on_hours: 41000, used_pct: 91 }),
  drive({ serial: 'C', name: 'charlie', health: 'caution', score: 55, temperature_c: 41, power_on_hours: 12000, used_pct: 70 }),
];

const names = (rows) => rows.map((d) => d.name);

// The default is not alphabetical and must not become so: the point of the table is that the drive
// needing attention is at the top of it.
test('the reading order puts what is failing first, then caution, then healthy', () => {
  assert.deepEqual(names(fleet.orderRows(FLEET, null)), ['bravo', 'charlie', 'alpha']);
});

test('a column sorts, and clicking it again reverses', () => {
  assert.deepEqual(names(fleet.orderRows(FLEET, { key: 'temp', dir: 1 })), ['alpha', 'charlie', 'bravo']);
  assert.deepEqual(names(fleet.orderRows(FLEET, { key: 'temp', dir: -1 })), ['charlie', 'alpha', 'bravo']);
});

// Sorting by a number and meeting a drive that has no number: it is not the coldest drive, and it is
// not the warmest either. It goes last, whichever way the column is pointing.
test('a drive with no value for the column sorts last in both directions', () => {
  const asc = fleet.orderRows(FLEET, { key: 'temp', dir: 1 });
  const desc = fleet.orderRows(FLEET, { key: 'temp', dir: -1 });
  assert.equal(asc[asc.length - 1].name, 'bravo');
  assert.equal(desc[desc.length - 1].name, 'bravo');
});

test('health sorts by verdict, and its reverse is healthiest first', () => {
  assert.deepEqual(names(fleet.orderRows(FLEET, { key: 'health', dir: 1 })), ['bravo', 'charlie', 'alpha']);
  assert.deepEqual(names(fleet.orderRows(FLEET, { key: 'health', dir: -1 })), ['alpha', 'charlie', 'bravo']);
});

test('name sorting is numeric-aware, so disk2 comes before disk10', () => {
  const rows = fleet.orderRows(
    [drive({ serial: '1', name: 'disk10' }), drive({ serial: '2', name: 'disk2' })],
    { key: 'drive', dir: 1 },
  );
  assert.deepEqual(names(rows), ['disk2', 'disk10']);
});

// The two halves of the table, and the way between them.
async function renderFleet(devices) {
  stubFetch({ devices, last_scan_ts: 1700000000000 });
  fleet.reset();
  const container = document.createElement('div');
  container.setAttribute('id', 'content');
  await fleet.render(container, { navigate() {}, refresh() {} });
  return container;
}

/** Every string in a subtree, in document order: the stub does not compute textContent for a parent. */
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
  return out.join(' ');
}
const buttons = (root, className) =>
  root.querySelectorAll('button').filter((b) => String(b.className).split(/\s+/).includes(className));

test('hidden drives are out of the fleet, and the count says how many', async () => {
  const container = await renderFleet([
    drive({ serial: 'A', name: 'alpha' }),
    drive({ serial: 'B', name: 'bravo', hidden: true }),
  ]);
  assert.match(textOf(container), /1 drive/);
  assert.match(textOf(container), /1 hidden/);
  assert.doesNotMatch(textOf(container), /bravo/);
});

test('the hidden half is one click away, and comes back the same way', async () => {
  const container = await renderFleet([
    drive({ serial: 'A', name: 'alpha' }),
    drive({ serial: 'B', name: 'bravo', hidden: true }),
  ]);

  const [toggle] = buttons(container, 'fleet-hidden-toggle');
  assert.ok(toggle, 'no way into the hidden half');
  toggle.onclick();

  assert.match(textOf(container), /bravo/, 'the hidden drive is still not drawn');
  assert.match(textOf(container), /Back to the fleet/);
  assert.doesNotMatch(textOf(container), /alpha/, 'the fleet and the hidden half are shown at once');

  buttons(container, 'fleet-hidden-toggle')[0].onclick();
  assert.doesNotMatch(textOf(container), /bravo/);
});

// The notes endpoint is a full replace, so a body carrying only `hidden` would clear the rest. This
// is the assertion that makes the guard worth having: the request has to carry every field the
// endpoint owns, with the row's own values in them.
test('hiding a drive sends the whole notes object, not just the flag', async () => {
  const requests = [];
  const row = drive({
    serial: 'A', name: 'alpha', notes: 'pulled for RMA', tags: 'media,archive', bay: 'bay 3',
  });
  stubFetch({ devices: [row], last_scan_ts: 0 }, requests);
  fleet.reset();
  const container = document.createElement('div');
  container.setAttribute('id', 'content');
  await fleet.render(container, { navigate() {}, refresh() {} });

  const [hide] = buttons(container, 'row-action');
  hide.onclick({ stopPropagation() {} });
  await new Promise((resolve) => setTimeout(resolve, 0));

  const put = requests.find((r) => r.method === 'PUT' && r.url.includes('/notes'));
  assert.ok(put, 'nothing was sent');
  assert.deepEqual(put.body, {
    name: 'alpha',
    notes: 'pulled for RMA',
    tags: 'media,archive',
    bay: 'bay 3',
    hidden: true,
  });
});

// A drive whose optional fields are absent must still send them as empty strings rather than leaving
// them out, which the endpoint would read as "clear this one" only because it was omitted at all.
test('a drive with nothing but the flag still sends every field', async () => {
  const requests = [];
  stubFetch({ devices: [drive({ serial: 'A', name: 'alpha' })], last_scan_ts: 0 }, requests);
  fleet.reset();
  const container = document.createElement('div');
  container.setAttribute('id', 'content');
  await fleet.render(container, { navigate() {}, refresh() {} });

  buttons(container, 'row-action')[0].onclick({ stopPropagation() {} });
  await new Promise((resolve) => setTimeout(resolve, 0));

  const put = requests.find((r) => r.method === 'PUT' && r.url.includes('/notes'));
  assert.deepEqual(Object.keys(put.body).sort(), ['bay', 'hidden', 'name', 'notes', 'tags']);
});

// Forgetting only makes sense for a drive that is not connected: for a connected one the next scan
// finds it again, so the button would look broken. The row for a missing drive carries both actions.
test('a disconnected drive offers Forget, a connected one does not', async () => {
  const container = await renderFleet([
    drive({ serial: 'A', name: 'alpha' }),
    drive({ serial: 'B', name: 'bravo', is_missing: true, health: 'unknown', score: -1 }),
  ]);
  const rows = container.querySelectorAll('tr').filter((r) => r.getAttribute('class') !== null || textOf(r).trim() !== '');
  const rowFor = (name) => rows.find((r) => textOf(r).includes(name));
  const actionsIn = (row) => row.querySelectorAll('button')
    .filter((b) => String(b.className).split(/\s+/).includes('row-action'))
    .map((b) => b.textContent || textOf(b));

  assert.deepEqual(actionsIn(rowFor('alpha')), ['Hide'], 'a connected drive must not offer Forget');
  assert.deepEqual(actionsIn(rowFor('bravo')), ['Hide', 'Forget']);
});

// Unhiding the last one: the reader got what they came for, so the table goes back rather than
// leaving an empty hidden half behind a toggle that now says "Back to the fleet" for nothing.
test('unhiding the last hidden drive returns to the fleet', async () => {
  let payload = {
    devices: [drive({ serial: 'A', name: 'alpha' }), drive({ serial: 'B', name: 'bravo', hidden: true })],
    last_scan_ts: 0,
  };
  // A stub that behaves like the endpoint: the PUT flips the flag, the next GET sees it.
  globalThis.fetch = async (url, init = {}) => {
    if (init.method === 'PUT' && String(url).includes('/notes')) {
      const body = JSON.parse(init.body);
      payload.devices = payload.devices.map((d) => (d.serial === 'B' ? { ...d, hidden: body.hidden } : d));
    }
    return { ok: true, status: 200, statusText: 'OK', text: async () => JSON.stringify(payload) };
  };

  fleet.reset();
  const container = document.createElement('div');
  container.setAttribute('id', 'content');
  await fleet.render(container, { navigate() {}, refresh() {} });

  buttons(container, 'fleet-hidden-toggle')[0].onclick();
  await new Promise((resolve) => setTimeout(resolve, 0));

  buttons(container, 'row-action')[0].onclick({ stopPropagation() {} });
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.match(textOf(container), /alpha/, 'the fleet did not come back');
  assert.doesNotMatch(textOf(container), /hidden/, 'still offering a way into an empty half');
});
