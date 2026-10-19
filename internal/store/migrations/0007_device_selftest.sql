-- 0007_device_selftest.sql — record the newest finished self-test result per device.
--
-- B9 notifies when a self-test finishes, and "finished" is a *transition*: it means the drive's
-- self-test log holds a result it did not hold last time we looked. Transition detection needs a
-- previous value to differ from, and that value has to survive a restart, so it is denormalized
-- here rather than re-derived from the raw output on every scan.
--
-- Nullable, and null means "never recorded" — deliberately different from the empty string. An
-- empty `selftest_type` is a drive whose log we read and which holds no finished test; null is a
-- drive we have not read since this migration ran. Only the first is a baseline a new result can
-- differ from, and collapsing the two would fire a notification for every pre-existing result the
-- moment someone upgraded.
--
-- `selftest_passed` is null for a result the drive gave no verdict on, which is not the same as a
-- failure. `selftest_hours` is the drive's lifetime hours when the test ran, and together with
-- the type it is what identifies one result from another.

ALTER TABLE devices ADD COLUMN selftest_type TEXT;
ALTER TABLE devices ADD COLUMN selftest_hours INTEGER;
ALTER TABLE devices ADD COLUMN selftest_passed INTEGER;
ALTER TABLE devices ADD COLUMN selftest_status TEXT;
