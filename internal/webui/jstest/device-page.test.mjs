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
const { render } = await import('../assets/js/views/device.js');

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
