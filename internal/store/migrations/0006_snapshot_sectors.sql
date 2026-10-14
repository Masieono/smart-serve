-- 0006_snapshot_sectors.sql — record the bad-sector total per snapshot.
--
-- Longevity and the worsening flag both need to know whether the number of bad sectors is
-- *moving*, and that is a question about history rather than about the latest reading. The
-- attributes blob already holds the counts, but SnapshotSeries deliberately does not load
-- that blob — it is heavy, and the trend chart asks for hundreds of rows — so the total is
-- denormalized into its own column.
--
-- Nullable, and left null for rows written before this migration: a projection needs two
-- points, and inventing a zero for history we never recorded would fabricate a flat trend
-- that was never observed. Null also means "this drive does not report sector counts",
-- which is true of NVMe.

ALTER TABLE snapshots ADD COLUMN sectors INTEGER;
