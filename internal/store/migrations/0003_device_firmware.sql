-- 0003_device_firmware.sql — persist the drive firmware revision, which is
-- useful for spotting known-bad firmware. Kept separate so 0001/0002 stay
-- immutable.

ALTER TABLE devices ADD COLUMN firmware TEXT NOT NULL DEFAULT '';
