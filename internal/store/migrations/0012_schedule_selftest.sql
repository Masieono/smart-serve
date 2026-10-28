-- 0012_schedule_selftest.sql — a schedule that runs self-tests, and which kind.
--
-- `A19`'s second half. The button is the primitive; a schedule is what turns it into a maintenance
-- routine, and it is the only path by which this app writes to a drive without somebody pressing
-- something (`FEATURES.md` A19, `ADR-024`).
--
-- The *kind* is stored rather than implied. A short test is minutes and a long test is hours, and a
-- schedule that picked one on the user's behalf would be choosing between "cheap, weekly" and
-- "expensive, quarterly" for them. It sits beside the action rather than in `kind`, which says
-- *when* the schedule runs: a self-test is monthly in practice, which is a cron expression, and
-- folding the action into `kind` would duplicate the whole timing machinery per action
-- (migration 0010).
--
-- DEFAULT 'short' so every existing row — none of which can be a self-test schedule, since the
-- action did not exist — gets the safe value rather than an unset one.

ALTER TABLE scan_schedules ADD COLUMN selftest_kind TEXT NOT NULL DEFAULT 'short';
