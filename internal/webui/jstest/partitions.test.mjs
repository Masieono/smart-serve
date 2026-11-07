// The partition schema (`FEATURES.md` A28), on the device page.
//
// The parsing and the alignment are tested in Go. What is pinned here is what the browser decides:
// which identifier a copy button offers, what "512e" means, and that the usage column says why it is
// empty rather than looking broken.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const { partitionTable, partitionUUID, sectorLabel, usageBar, outsideContainerNote, setStorageWarnPct, storageWarnLevel, diskMap } =
  await import('../assets/js/components.js');
const { setDisplayOptions, currentDisplayOptions, diskLayoutCard } =
  await import('../assets/js/views/device.js');

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

// A filesystem the container cannot see has no figure, and says so in the cell. A dash would be read
// as a bug, and a zero would be read as an empty filesystem — which is the opposite of the truth.
test('usage the container cannot see says so in the cell', () => {
  const layout = {
    partitions: [partition({ use_percent: '', used_bytes: undefined, fs_size_bytes: undefined })],
  };
  const text = textOf(partitionTable(layout));

  assert.match(text, /not visible/);
  assert.doesNotMatch(text, /0%/, 'an unknown figure must not be drawn as an empty filesystem');
});

// The explanation used to be a paragraph under the table, which made the card read as a wall of text.
// It is now a string the Details disclosure carries, so it is worth asserting on directly.
test('the outside-container explanation names the fix', () => {
  const partitions = [partition({ use_percent: '', used_bytes: undefined })];
  const note = outsideContainerNote(partitions);

  assert.match(note, /not inside this container/);
  assert.match(note, /mnt:\/mnt:ro/);
  // Singular reads as a sentence, not as "1 of these filesystems are".
  assert.match(outsideContainerNote([partition({ use_percent: '', used_bytes: undefined })]),
    /1 of these filesystems is/);
  // Nothing hidden: no note at all, rather than a note saying zero.
  assert.equal(outsideContainerNote([partition()]), '');
  assert.equal(outsideContainerNote([]), '');
});

// The bar is the visualisation, and the one thing it must never do is draw a length for a figure
// nobody measured. `Number(null)` is 0, which is how this went wrong the first time.
test('the usage bar refuses a figure that is not a number', () => {
  assert.equal(usageBar(null), null);
  assert.equal(usageBar(undefined), null);
  assert.equal(usageBar(''), null);
  assert.equal(usageBar('not-a-number'), null);
  assert.equal(usageBar(Number.NaN), null);

  // And a real zero *is* a figure, so it gets a bar.
  assert.ok(usageBar(0));
  assert.match(textOf(usageBar(0)), /0%/);
});

test('the usage bar clamps and labels itself', () => {
  assert.match(textOf(usageBar(47)), /47%/);
  assert.match(textOf(usageBar(47.6)), /48%/, 'rounded, not truncated');
  assert.match(textOf(usageBar(140)), /100%/, 'a nonsense figure cannot overflow the track');
  assert.match(textOf(usageBar(-5)), /0%/);
  assert.match(textOf(usageBar(47, { label: '/mnt/media' })), /\/mnt\/media/);
});

/** The fill's class, which is where the amber level shows. */
function fillClass(node) {
  const track = node.all('div').find((n) => n.className === 'usage-track');
  return track.all('div')[0].className;
}

// The level travels with the other display preferences, because that is what it is. The device page
// applies it once at boot, so every bar on every page uses the same level.
test('the warning level arrives with the display preferences', async () => {
  setDisplayOptions({ storage_warn_pct: '75', show_usage: 'true' });
  assert.equal(storageWarnLevel(), 75, 'the stored level was not applied');

  // And an older server that does not send it falls back rather than turning every bar amber.
  setDisplayOptions({});
  assert.equal(storageWarnLevel(), 90);

  setDisplayOptions({ storage_warn_pct: '90' });
});
// The bar turns amber at the level the user set — a stored setting rather than a constant here,
// because the browser deciding on its own where "nearly full" begins would be the UI inventing a
// judgement.
test('the usage bar turns amber at the warning level, and not before', () => {
  setStorageWarnPct(90);
  assert.doesNotMatch(fillClass(usageBar(89)), /warn/);
  assert.match(fillClass(usageBar(90)), /warn/, 'the level itself is past the line');
  assert.match(fillClass(usageBar(100)), /warn/);

  // A stored level is honoured: 70 means 70, not 90.
  setStorageWarnPct(70);
  assert.equal(storageWarnLevel(), 70);
  assert.match(fillClass(usageBar(70)), /warn/);
  assert.doesNotMatch(fillClass(usageBar(69)), /warn/);

  // A level that cannot be honoured falls back to the default rather than turning every bar amber.
  for (const junk of ['nonsense', '', null, 0, 250]) {
    setStorageWarnPct(junk);
    assert.equal(storageWarnLevel(), 90, `setStorageWarnPct(${JSON.stringify(junk)}) was accepted`);
  }
  assert.doesNotMatch(fillClass(usageBar(50)), /warn/);
  setStorageWarnPct(90);
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

// A drive with no partition table: what it *is* beats "unpartitioned or unreadable" (`FEATURES.md`
// A28). A disk pulled from a RAID array has no table because it is an array member, and saying that
// turns a message that reads like a fault into a description of a drive doing its job.
test('a drive with no partition table says what it actually holds', () => {
  // A `<p>` is the whole answer here, so its text is on the element itself rather than in children.
  const note = (layout) => String(partitionTable(layout).textContent);

  // An array member: the plain-language name, the exact blkid token, and no implication of a fault.
  const raid = note({
    whole_disk_fstype: 'linux_raid_member',
    whole_disk_name: 'a Linux md array',
    partitions: [],
  });
  assert.match(raid, /member of a Linux md array/);
  assert.match(raid, /linux_raid_member/, 'the exact token is what somebody will search for');
  assert.match(raid, /Nothing here is a fault/);

  // A filesystem written straight to the device: no table, but plenty to say.
  const whole = note({ whole_disk_fstype: 'ext4', partitions: [] });
  assert.match(whole, /ext4/);
  assert.match(whole, /written directly to the disk/);

  // Nothing reported at all: the old message, which is the honest answer for a drive nobody has set
  // up. It must not claim to know more than it does.
  const unknown = note({ partitions: [] });
  assert.match(unknown, /unpartitioned or its table could not be read/);
});

// A drive with partitions never gets the whole-disk sentence, even if the payload carries a signature
// — a partitioned disk's signature *is* its table, and saying otherwise would be doubly wrong.
test('a partitioned drive never shows the whole-disk note', () => {
  const layout = {
    whole_disk_fstype: 'linux_raid_member',
    whole_disk_name: 'a Linux md array',
    partitions: [partition({ name: '/dev/sdd1' })],
  };
  const text = textOf(partitionTable(layout));
  assert.doesNotMatch(text, /member of/);
  assert.match(text, /\/dev\/sdd1/, 'the table itself is still drawn');
});

// --- the disk map (`FEATURES.md` A28) ------------------------------------------

/** The rendered segments of a map, in order. */
function segmentsOf(map) {
  return map.all('div').filter((n) => /(^| )disk-segment( |$)/.test(n.className));
}

/** A layout with the fields the map reads, sized in bytes. */
function mapped(partitions, total) {
  return { size_bytes: total, partitions };
}

/**
 * What a segment's label says once the mouse is over it, and that the segment marked itself.
 *
 * `mouseenter` rather than `mouseover`: the segment has a child (the fill), and the bubbling pair
 * fires again every time the pointer crosses into it — which flickers the label and the row.
 */
function tooltipFor(seg) {
  seg.dispatch('mouseenter');
  assert.match(seg.className, /is-hover/, 'the hovered segment did not mark itself');
  const tip = seg.parent.all('div').find((n) => n.className === 'disk-tooltip');
  assert.ok(tip, 'the map has no tooltip');
  assert.equal(tip.hidden, false, 'the tooltip stayed hidden on hover');
  return String(tip.textContent);
}

const GIB = 1024 ** 3;

// Pointing at a segment marks the table row it stands for (`FEATURES.md` A28). The map is the shape
// and the table is the detail; without this the reader matches a bar to a path by eye.
//
// Matched by the drive's own name for the partition rather than by row order, which matters on a drive
// whose table also carries a row per logical volume — more rows than the map has segments, and
// deliberately so.
test('pointing at a segment marks the row it stands for', () => {
  const layout = mapped([
    { top_level: true, name: '/dev/sdb1', size_bytes: 400 * GIB, use_percent: '20%' },
    { top_level: true, name: '/dev/sdb2', size_bytes: 600 * GIB, use_percent: '80%' },
    { name: '/dev/mapper/vg-data', size_bytes: 500 * GIB, use_percent: '90%' },
  ], 1000 * GIB);

  const card = diskLayoutCard(layout, { showUsage: true });
  const segs = card.all('div').filter((n) => /(^| )disk-segment( |$)/.test(n.className));
  const rows = card.all('tr');

  segs[1].dispatch('mouseenter');
  const marked = rows.filter((r) => /is-hover/.test(r.className));
  assert.equal(marked.length, 1, `${marked.length} rows were marked, want exactly one`);
  assert.equal(marked[0].getAttribute('data-partition'), '/dev/sdb2');

  // And letting go clears it, so the marker does not follow the reader around the page.
  segs[1].dispatch('mouseleave');
  assert.equal(rows.filter((r) => /is-hover/.test(r.className)).length, 0);
});

// The heading used to carry the partition count on its own — a bare number with nothing saying what it
// counted. The map answers the question the count was reaching for, and a number nobody can label is
// not worth a corner of the card.
test('the partition heading carries no bare count', () => {
  const card = diskLayoutCard(mapped([
    { top_level: true, name: '/dev/sdb1', size_bytes: 400 * GIB, use_percent: '20%' },
    { top_level: true, name: '/dev/sdb2', size_bytes: 600 * GIB, use_percent: '80%' },
  ], 1000 * GIB), { showUsage: true });

  const heading = card.all('h2')[0];
  assert.equal(String(heading.textContent), 'Partitions');
  const row = card.all('div').find((n) => /(^| )row( |$)/.test(n.className));
  assert.equal(row.all('span').filter((n) => /muted/.test(n.className)).length, 0,
    'a bare count is still in the heading');
});

// A drive with no partitions is the note, not an empty map: an empty bar would claim the disk has
// nothing on it, which is a different statement from "there is no table to draw".
test('a drive with no layout gets no map', () => {
  assert.equal(diskLayoutCard(null, {}), null);
});
test('the map sizes each segment by its share of the disk', () => {
  const map = diskMap(mapped([
    { top_level: true, name: '/dev/sdb1', size_bytes: 250 * GIB, use_percent: '8%' },
    { top_level: true, name: '/dev/sdb2', size_bytes: 750 * GIB, use_percent: '47%' },
  ], 1000 * GIB));

  const segs = segmentsOf(map);
  assert.equal(segs.length, 2, 'no free-space segment: the partitions claim the whole disk');
  assert.match(String(segs[0].style), /width:25%/, 'the 250 GiB partition is a quarter of the disk');
  assert.match(String(segs[1].style), /width:75%/);
  // And the fill inside a segment is its own fullness, not its share.
  const fill = segs[1].all('div')[0];
  assert.match(String(fill.style), /width:47%/, 'the segment fill is the partition usage');
});

// A logical volume lives *inside* a partition. Drawing it beside its container would claim the disk
// holds more than it does, so only top-level nodes become segments.
test('the map draws top-level partitions only', () => {
  const map = diskMap(mapped([
    { top_level: true, name: '/dev/sdb1', size_bytes: 200 * GIB, use_percent: '10%' },
    { top_level: true, name: '/dev/sdb2', size_bytes: 800 * GIB },
    { name: '/dev/mapper/vg-data', size_bytes: 700 * GIB, use_percent: '90%' },
  ], 1000 * GIB));

  const segs = segmentsOf(map);
  assert.equal(segs.length, 2, 'the nested volume was drawn as if it were its own partition');
  const text = segs.map((s) => s.getAttribute('title')).join(' | ');
  assert.doesNotMatch(text, /vg-data/, 'the nested volume leaked into the map');
});

// Free space is the difference, and it is approximate — so it gets its own segment and says so.
test('the map shows free space as approximate', () => {
  const map = diskMap(mapped([
    { top_level: true, name: '/dev/sdb1', size_bytes: 400 * GIB, use_percent: '20%' },
  ], 1000 * GIB));

  const segs = segmentsOf(map);
  assert.equal(segs.length, 2);
  const free = segs[1];
  assert.match(free.className, /disk-segment-free/);
  assert.match(String(free.style), /width:60%/);
  assert.match(tooltipFor(free), /approximate/, 'the free figure must say it is approximate');
});

// Unknown fullness is not zero. A partition whose filesystem the container cannot see is hatched,
// because a bar at 0% would say "this partition is empty" about one nobody could look at.
test('an unmeasured partition is hatched, not drawn at zero', () => {
  const map = diskMap(mapped([
    { top_level: true, name: '/dev/sdb1', size_bytes: 500 * GIB, mountpoints: ['/mnt/x'] },
  ], 1000 * GIB));

  const fill = segmentsOf(map)[0].all('div')[0];
  assert.match(fill.className, /disk-segment-unmeasured/);
  assert.ok(!String(fill.style || '').includes('width'),
    'an unmeasured partition must not be given a width');
});

// The whole thing is geometry, so the sentence a segment carries is what a screen reader gets — and
// what a mouse gets. Both have to be complete enough to be worth having.
//
// The mouse gets a tooltip of the app's own rather than the `title` attribute: the browser's takes
// about a second to appear, and a map is something you sweep across.
test('a segment names its partition, its size and its usage', () => {
  const map = diskMap(mapped([
    { top_level: true, name: '/dev/sdb1', label: 'media', size_bytes: 500 * GIB,
      mountpoints: ['/mnt/media'], use_percent: '47%' },
  ], 1000 * GIB));

  const seg = segmentsOf(map)[0];
  assert.match(seg.getAttribute('aria-label'), /media/);
  assert.match(seg.getAttribute('aria-label'), /\/mnt\/media/);
  assert.match(seg.getAttribute('aria-label'), /47% full/);
  assert.ok(!seg.getAttribute('title'),
    'a `title` would give a second tooltip, one of them a second late');

  assert.match(tooltipFor(seg), /47% full/, 'the hover label is not populated');
});

// Nothing to draw is not an empty bar: a caller that gets null renders the table alone, where an
// empty bar would claim the disk has no partitions.
test('the map refuses to draw when there is nothing to map', () => {
  assert.equal(diskMap(null), null);
  assert.equal(diskMap({ partitions: [] }), null);
  assert.equal(diskMap(mapped([{ top_level: true, name: '/dev/sdb1', size_bytes: 0 }], 1000 * GIB)), null);
  // No total size: there is no denominator, so every share would be a division by zero.
  assert.equal(diskMap(mapped([{ top_level: true, name: '/dev/sdb1', size_bytes: 100 }], 0)), null);
  // Only nested nodes, which never become segments.
  assert.equal(diskMap(mapped([{ name: '/dev/mapper/vg', size_bytes: 100 }], 1000 * GIB)), null);
});

// An alignment remainder is not free space. The 1.4 MB left after a partition table is real, and
// drawing it as a labelled segment would give a visible sliver to a number nobody acts on — the same
// figure that was confusing in the card's old summary line.
test('the map does not draw a segment for an alignment remainder', () => {
  const map = diskMap(mapped([
    { top_level: true, name: '/dev/sdb1', size_bytes: 1000 * GIB - 1400832, use_percent: '20%' },
  ], 1000 * GIB));

  assert.equal(segmentsOf(map).length, 1, 'a 1.4 MB remainder was given its own segment');
  assert.doesNotMatch(textOf(map), /free/);
});

// An extended partition makes its children add up to more than the disk. The map must clamp rather
// than render a negative-width free segment.
test('the map clamps free space instead of going negative', () => {
  const map = diskMap(mapped([
    { top_level: true, name: '/dev/sdb1', size_bytes: 600 * GIB, use_percent: '10%' },
    { top_level: true, name: '/dev/sdb2', size_bytes: 600 * GIB, use_percent: '10%' },
  ], 1000 * GIB));

  assert.equal(segmentsOf(map).length, 2, 'a negative free segment was drawn');
});
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

