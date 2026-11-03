-- 0016_snapshot_used_pct.sql — how full the drive was, per snapshot.
--
-- `FEATURES.md` A28. Storage usage is the one figure on the partition card that is worth watching
-- over time: a NAS drive that is 40% full today and 90% full next month is the thing that runs out
-- of room, and that is a question about history rather than about the latest reading.
--
-- Denormalized into its own column for exactly the reason `sectors` was (migration 0006):
-- `SnapshotSeries` deliberately does not load the `attributes` blob, because the trend chart asks for
-- hundreds of rows and that blob is heavy. The partition schema lives on the *device*, not in the
-- snapshot, so there is nothing to read at chart time at all without this.
--
-- The value is the *fullest* filesystem on the drive, not an average: a drive holding a 20%-full boot
-- partition and a 95%-full data volume is a drive about to run out of space, and a blended figure
-- would hide the filesystem somebody needs to be told about. That choice is made once, in
-- `smart.DiskLayout.Fullest`, and this column is its history.
--
-- NULL rather than 0 when there is no figure: a drive whose filesystems the container cannot see has
-- not been measured, and 0 would read as "empty".

ALTER TABLE snapshots ADD COLUMN used_pct INTEGER;
