-- 0002_device_mountpoints.sql — persist lsblk context for the UI cards.
-- Kept as a separate migration so the released 0001 stays immutable.

ALTER TABLE devices ADD COLUMN mountpoints TEXT NOT NULL DEFAULT '[]';
ALTER TABLE devices ADD COLUMN fstype TEXT NOT NULL DEFAULT '';
