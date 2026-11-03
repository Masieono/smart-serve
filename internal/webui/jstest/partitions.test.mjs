// The partition schema (`FEATURES.md` A28), on the device page.
//
// The parsing and the alignment are tested in Go. What is pinned here is what the browser decides:
// which identifier a copy button offers, what "512e" means, and that the usage column says why it is
// empty rather than looking broken.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const { partitionTable, partitionUUID, sectorLabel } = await import('../assets/js/components.js');
const { setDisplayOptions, currentDisplayOptions } = await import('../assets/js/views/device.js');

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

const partition = (over = {}) => ({
  name: '/dev/sda1',
  type: 'part',
  size_bytes: 536870912,
  fstype: 'vfat',
  fs_version: 'FAT32',
  mountpoints: ['/boot/efi'],
  use_percent: '8%',
  used_bytes: 42949672,
  fs_size_bytes: 536870912,
  ...over,
});

const LAYOUT = {
  table: 'gpt',
  log_sector_size: 512,
  phy_sector_size: 4096,
  discard_support: true,
  unallocated_bytes: 1074000000,
  partitions: [
    partition({
      label: 'EFI', part_type: 'EFI System', uuid: '1A2B-3C4D',
      part_uuid: '6f8b1c2d-3e4f-4a5b-8c9d-0e1f2a3b4c5d',
    }),
    partition({
      name: '/dev/mapper/tank-media', type: 'lvm', label: 'media', fstype: 'ext4',
      fs_version: '1.0', mountpoints: ['/mnt/media'], use_percent: '47%', part_uuid: '',
    }),
  ],
};

// --- what a copy button offers -------------------------------------------------

// A filesystem UUID is what fstab wants (`UUID=`); a partition UUID is the fallback for a partition
// with no filesystem — a volume-group member, which is exactly the row somebody copies one from.
test('the copy button prefers the filesystem UUID and falls back to the partition UUID', () => {
  assert.equal(partitionUUID({ uuid: 'AAAA', part_uuid: 'BBBB' }), 'AAAA');
  assert.equal(partitionUUID({ part_uuid: 'BBBB' }), 'BBBB');
  assert.equal(partitionUUID({ uuid: '  AAAA  ' }), 'AAAA', 'trimmed');
  // Neither: no button, rather than a button that copies nothing.
  assert.equal(partitionUUID({}), '');
  assert.equal(partitionUUID(null), '');
});

// --- sector sizes --------------------------------------------------------------

// Which label applies is decided by the *pair*: 512 on 4096 is 512e, 4096 on 4096 is 4Kn, and 512 on
// 512 is neither — a drive that is not emulating anything gets no label rather than a wrong one.
test('sector sizes are named the way people say them', () => {
  assert.equal(sectorLabel(512, 4096), '512e');
  assert.equal(sectorLabel(4096, 4096), '4Kn');
  assert.equal(sectorLabel(512, 512), '');
  assert.equal(sectorLabel(4096, 512), '', 'a pair that should not exist gets no guess');
  assert.equal(sectorLabel(0, 4096), '');
  assert.equal(sectorLabel(undefined, undefined), '');
});

// --- the table -----------------------------------------------------------------

test('the table names the label first and the node name under it', () => {
  const text = textOf(partitionTable(LAYOUT));

  assert.match(text, /EFI/);
  assert.match(text, /\/dev\/sda1/, 'the node name is still there for whoever needs it');
  assert.match(text, /media/);
  assert.match(text, /\/dev\/mapper\/tank-media/);
  assert.match(text, /vfat FAT32/);
  assert.match(text, /ext4 1\.0/);
  assert.match(text, /\/mnt\/media/);
  assert.match(text, /EFI System/);
});

// The usage column is the setting's to remove, and it is removed rather than emptied: an empty
// column is a worse way to say "you asked not to see this" than no column at all.
test('the usage column goes away entirely when it is turned off', () => {
  assert.match(textOf(partitionTable(LAYOUT, { showUsage: true })), /47%/);
  assert.doesNotMatch(textOf(partitionTable(LAYOUT, { showUsage: false })), /47%/);
});

// A filesystem the container cannot see has no figure, and says so. A dash would be read as a bug,
// and a zero would be read as an empty filesystem — which is the opposite of the truth.
test('usage the container cannot see says why, once', () => {
  const layout = {
    partitions: [partition({ use_percent: '', used_bytes: undefined, fs_size_bytes: undefined })],
  };
  const text = textOf(partitionTable(layout));

  assert.match(text, /not visible/);
  // Said once, with the fix, rather than repeated per row.
  assert.match(text, /mount roots|read-only/);
});

// A partition that is mounted nowhere and has no usage is not "not visible" — there is nothing to
// measure. Saying otherwise would send somebody to their compose file for no reason.
test('an unmounted partition is not reported as invisible', () => {
  const layout = {
    partitions: [partition({ mountpoints: [], use_percent: '', used_bytes: undefined })],
  };
  const text = textOf(partitionTable(layout));

  assert.match(text, /not mounted/);
  assert.doesNotMatch(text, /not visible/);
});

// A drive with no table at all is a fact about the drive, not a failure — and an unpartitioned disk
// is one nobody has set up yet.
test('a drive with no partitions says so', () => {
  assert.match(String(partitionTable({ partitions: [] }).textContent), /no partition table/i);
  assert.equal(partitionTable(null).tagName, 'P');
});

// A read-only device is worth a badge: it explains a write-protect switch, or a drive that has
// failed into read-only, without the reader having to know that `RO` exists.
test('a read-only partition is marked', () => {
  assert.match(textOf(partitionTable({ partitions: [partition({ read_only: true })] })), /Read-only/);
  assert.doesNotMatch(textOf(partitionTable({ partitions: [partition()] })), /Read-only/);
});

// --- the two display settings --------------------------------------------------

// Anything other than an explicit "false" leaves a feature on, so a missing setting, a typo or an
// older server all read as the default rather than as "hide".
test('the display settings only hide on an explicit false', () => {
  setDisplayOptions({});
  assert.deepEqual(currentDisplayOptions(), { showPartitions: true, showUsage: true });

  setDisplayOptions({ show_partitions: 'false', show_usage: 'false' });
  assert.deepEqual(currentDisplayOptions(), { showPartitions: false, showUsage: false });

  setDisplayOptions({ show_partitions: 'FALSE', show_usage: 'true' });
  assert.deepEqual(currentDisplayOptions(), { showPartitions: false, showUsage: true });

  // Nonsense is not "hide".
  setDisplayOptions({ show_partitions: 'maybe', show_usage: '' });
  assert.deepEqual(currentDisplayOptions(), { showPartitions: true, showUsage: true });
});

