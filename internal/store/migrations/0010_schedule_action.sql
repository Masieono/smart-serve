-- 0010_schedule_action.sql — what a schedule does, not just when it runs.
--
-- A schedule has always been a *scan* schedule: `kind` says how often (interval or cron) and the
-- action was implied. A weekly digest (`FEATURES.md` B13) needs the other axis — a summary on a
-- schedule is exactly the same timing problem as a scan on a schedule — so `action` names what
-- happens when the boundary arrives.
--
-- Two axes rather than a third `kind`, deliberately. A digest is weekly, which is a cron expression;
-- folding the action into `kind` would mean either duplicating the interval/cron machinery per action
-- or a `kind` of "digest" that says nothing about when. Keeping them separate means the digest
-- inherits every timing feature the scan schedules already have: cron, time zones, and the missed-run
-- policy.
--
-- NOT NULL DEFAULT 'scan' so every existing row means what it already meant, and so an older client
-- that does not know about `action` keeps creating scan schedules.

ALTER TABLE scan_schedules ADD COLUMN action TEXT NOT NULL DEFAULT 'scan';
