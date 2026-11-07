// The device page, rendered.
//
// That sounds like a low bar and it is exactly the bar that was missed. `storageBar` was referenced
// from the render path and never defined, so every drive page carrying a storage summary threw
// `storageBar is not defined` and showed an error box instead of the drive. `node --check` passes on
// an undefined reference — it is valid syntax — and every existing test either checked the helper in
// isolation or never rendered this page at all. So it reached a person.
//
// The rule this test exists to enforce: **a view that can throw needs one test that renders it.**
// Helpers are tested in isolation and that is right, but isolation is precisely what cannot see a
// missing name.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const { render, setDisplayOptions, startFreshness } = await import('../assets/js/views/device.js');
const { el } = await import('../assets/js/dom.js');

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

/** A device payload with everything the page reads, and a storage summary to draw. */
function devicePayload(over = {}) {
  return {
    bench_tag: 'bench',
    metrics: {},
    projection: null,
    snapshot: null,
    device: {
      serial: 'SN-1',
      model: 'Samsung SSD 870 EVO',
      device_path: '/dev/sdb',
      transport: 'sata',
      health: 'good',
      health_rationale: 'everything within range',
      score: 96,
      smart_available: true,
      mountpoints: ['/mnt/media'],
      fstype: 'ext4',
      fs_version: '1.0',
      first_seen: '2026-01-01T00:00:00Z',
      last_scan: '2026-02-01T00:00:00Z',
      tags: [],
      storage: { mount: '/mnt/media', used_pct: 47, used_bytes: 469060000000, total_bytes: 999000000000 },
      disk_layout: {
        table: 'gpt',
        log_sector_size: 512,
        phy_sector_size: 4096,
        discard_support: true,
        partitions: [
          { name: '/dev/sdb1', type: 'part', size_bytes: 536870912, fstype: 'vfat',
            label: 'EFI', part_type: 'EFI System', mountpoints: ['/boot/efi'], use_percent: '8%' },
          { name: '/dev/sdb2', type: 'part', size_bytes: 999000000000, fstype: 'ext4',
            label: 'media', part_type: 'Linux filesystem', mountpoints: ['/mnt/media'],
            use_percent: '47%', used_bytes: 469060000000, fs_size_bytes: 999000000000 },
        ],
      },
      ...over,
    },
  };
}

/** Routes the two requests the device page makes, which want different payloads. */
function stubRoutes({ device = devicePayload(), history = { series: [] } } = {}) {
  globalThis.fetch = async (url) => {
    const body = String(url).includes('/history') ? history : device;
    return {
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify(body),
    };
  };
}

/** Lets queued promise callbacks run, which is how a test sees a read's effect. */
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

// The freshness line, and the one read this app makes without anybody asking (`FEATURES.md` A28
// follow-up).
//
// Everything on a drive page comes from the last *scan*, and "4 hours ago" in a row of twelve facts is
// not something a reader notices. So the age goes beside the drive's own name — and when it is past
// the window the user chose, the page reads that one drive and redraws.
//
// The gate is the point: this is the only place the app touches a drive nobody asked it to touch, and
// on a machine that parks its disks that is exactly the thing not to do.
test('a fresh drive is not re-read', () => {
  setDisplayOptions({ device_refresh_minutes: '15' });
  const ctx = freshCtx();

  const line = el('div', {});
  startFreshness(ctx, { serial: 'S', last_scan: new Date().toISOString() }, line);

  assert.match(textOf(line), /data from/);
  assert.equal(ctx.requests.length, 0, 'a drive scanned a moment ago was read again');
});

// Past the window the page reads the drive — once — and redraws so the fresh verdict, score and
// partition table all arrive together.
test('a stale drive is re-read, and the page says so while it happens', async () => {
  setDisplayOptions({ device_refresh_minutes: '15' });
  const ctx = freshCtx();
  const long = new Date(Date.now() - 4 * 3600 * 1000).toISOString();

  const line = el('div', {});
  startFreshness(ctx, { serial: 'S', last_scan: long }, line);

  // Before the read lands, the line explains itself rather than letting a figure that is about to
  // change appear without warning.
  assert.match(textOf(line), /reading the drive now/);

  await flush();
  const posts = ctx.requests.filter((r) => r.method === 'POST');
  assert.equal(posts.length, 1, `${posts.length} reads were made, want one`);
  assert.match(posts[0].path, /\/api\/devices\/S\/scan$/);
  assert.equal(ctx.refreshed, 1, 'the page did not redraw with the fresh data');
});

// Zero is "never", which is the setting for a machine that parks its disks. Nothing is read, and the
// age is still shown — the reader can see it is old and press Scan themselves.
test('a zero window never re-reads a drive', () => {
  setDisplayOptions({ device_refresh_minutes: '0' });
  const ctx = freshCtx();
  const long = new Date(Date.now() - 4 * 3600 * 1000).toISOString();

  const line = el('div', {});
  startFreshness(ctx, { serial: 'S', last_scan: long }, line);

  assert.equal(ctx.requests.length, 0, 'a drive was read with the window turned off');
  assert.match(textOf(line), /data from/, 'the age should still be visible');
});

// A drive that cannot be read is not a reason to blank the page: the stored data is still the last
// thing the drive said. The line says what happened instead of leaving "reading the drive now…" up
// for good.
test('a failed re-read says so and leaves the page alone', async () => {
  setDisplayOptions({ device_refresh_minutes: '15' });
  const ctx = freshCtx({ failScan: true });
  const long = new Date(Date.now() - 4 * 3600 * 1000).toISOString();

  const line = el('div', {});
  startFreshness(ctx, { serial: 'S', last_scan: long }, line);
  await flush();

  assert.match(textOf(line), /could not re-read/);
  assert.equal(ctx.refreshed, 0, 'the page was redrawn for a read that failed');
});

// A drive that has never been scanned has nothing to show, so reading it *is* the point of opening the
// page. The staleness window exists to stop re-reads; it has nothing to say about a first read, and
// applying it here would leave the page empty for no reason.
test('a drive with no data at all is read whatever the window says', async () => {
  setDisplayOptions({ device_refresh_minutes: '0' });
  const ctx = freshCtx();

  const line = el('div', {});
  startFreshness(ctx, { serial: 'S', last_scan: null }, line);

  assert.match(textOf(line), /reading the drive now/);
  await flush();
  assert.equal(ctx.requests.filter((r) => r.method === 'POST').length, 1,
    'a drive with no data was left with an empty page');
});

/** A context that records what the freshness check did. */
function freshCtx(opts = {}) {
  const requests = [];
  globalThis.fetch = async (url, init = {}) => {
    requests.push({ path: String(url), method: init.method || 'GET' });
    if (opts.failScan) return { ok: false, status: 502, statusText: 'Bad Gateway', text: async () => '' };
    return { ok: true, status: 200, statusText: 'OK', text: async () => '{}' };
  };
  return {
    requests,
    refreshed: 0,
    refresh() {
      this.refreshed++;
    },
  };
}

async function renderDevice(device, history) {
  stubRoutes({ device, history });
  const container = globalThis.document.createElement('div');
  const ctx = { refresh() {}, navigate() {}, scanDevice() {}, setDisplayOptions() {} };
  await render(container, ctx, 'SN-1');
  return container;
}

// The regression itself, stated as the thing a person saw: the page must not be an error box.
test('the device page renders rather than throwing', async () => {
  const container = await renderDevice(devicePayload());
  const text = textOf(container);

  assert.doesNotMatch(text, /Could not load device/,
    'the page failed to render; this is what "storageBar is not defined" looked like to the user');
  assert.match(text, /Samsung SSD 870 EVO/, 'the drive did not render at all');
});

// And the storage summary actually reaching the screen — a page that renders without the feature it
// was supposed to show would pass the test above.
test('the storage bar and the partition table reach the page', async () => {
  const text = textOf(await renderDevice(devicePayload()));

  assert.match(text, /47%/, 'the storage bar is missing');
  assert.match(text, /\/mnt\/media/, 'the storage bar does not say which filesystem it is');
  assert.match(text, /Partitions/, 'the partition card is missing');
  assert.match(text, /EFI System/, 'the partition type is missing');
});

// The other half of the same mistake: a drive the container cannot measure must still render. This is
// the shape the two USB drives report, so it is the case a person will actually hit.
test('a drive with no measurable filesystem still renders', async () => {
  const payload = devicePayload({ storage: undefined, disk_layout: undefined });
  const text = textOf(await renderDevice(payload));

  assert.doesNotMatch(text, /Could not load device/);
  assert.match(text, /Samsung SSD 870 EVO/);
  assert.doesNotMatch(text, /Storage/, 'a storage row was drawn for a drive with no figure');
});
