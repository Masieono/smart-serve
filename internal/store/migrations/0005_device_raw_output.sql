-- 0005_device_raw_output.sql — keep the last raw smartctl output per drive.
--
-- It used to live only in the scanner's in-memory result, so it was gone after every
-- restart and was clobbered by a single-drive rescan — precisely the situations where
-- you most want to see what smartctl actually said.
--
-- Kept on `devices` (latest only) rather than per-snapshot, so it cannot grow without
-- bound. Kept as a separate migration so the released 0001-0004 stay immutable.

ALTER TABLE devices ADD COLUMN raw_output TEXT NOT NULL DEFAULT '';