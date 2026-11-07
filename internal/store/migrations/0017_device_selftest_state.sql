-- 0017_device_selftest_state.sql — whether the drive is testing itself right now.
--
-- `FEATURES.md` A19/A20. The self-test card already reads this live from the drive while somebody is
-- looking at it, but that only helps on the page you are on: start a long test, look at another
-- drive, and the fleet gave no sign of which one was busy. The reported symptom was pressing Run on
-- the wrong drive and being told one was already going.
--
-- So the scan records it. It already reads each drive's self-test log — it parses the log for the
-- *finished* result it stores in `selftest_*` — and a test in progress is the same log's entry that
-- carries a percentage remaining. Keeping it costs three columns and one read that was happening
-- anyway.
--
-- `selftest_pct` is NULL when the drive does not report progress. Many drives only write a log entry
-- when a test *finishes*, so "running, no figure" is a real and common state — and 0 would claim a
-- test at its very beginning.
--
-- These are overwritten on every scan rather than COALESCEd like the finished result: a test that has
-- ended must stop showing as running, and a stale badge is worse than no badge.

ALTER TABLE devices ADD COLUMN selftest_running INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN selftest_pct INTEGER;
ALTER TABLE devices ADD COLUMN selftest_kind TEXT NOT NULL DEFAULT '';
