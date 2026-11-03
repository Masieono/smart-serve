-- 0015_device_disk_layout.sql — the partition schema, kept instead of flattened away.
--
-- `FEATURES.md` A28. `lsblk` has always been asked for the whole device tree, and `collectTree`
-- walked every partition of it — only to gather mountpoints and filesystems into two strings, after
-- which the sizes, types, labels, offsets and usage were discarded. They were paid for on every scan
-- and never seen.
--
-- A JSON blob, like `mountpoints` and the snapshot attributes, and for the same reason: it is read
-- whole, never queried by, and every field in it comes from the same single `lsblk` call. It holds
-- the partitions plus the handful of storage facts that belong with them (partition table type,
-- sector sizes, discard support).
--
-- DEFAULT '' rather than '[]' so "never read" stays distinguishable from "read, and there are no
-- partitions" — the second is a fact about the drive, and an unpartitioned disk is one nobody has
-- set up yet.

ALTER TABLE devices ADD COLUMN disk_layout TEXT NOT NULL DEFAULT '';
