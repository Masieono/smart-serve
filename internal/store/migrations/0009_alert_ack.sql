-- 0009_alert_ack.sql — acknowledging a condition the user already knows about.
--
-- The problem B7 solves: a drive with a known issue alerts on every scan until somebody fixes it, and
-- the only ways to stop it were to disable the channel or hide the drive. Both throw away the
-- monitoring. An acknowledgement says "I know about this one" and leaves everything else alone.
--
-- Three columns on `device_notes` rather than a table of its own, because an acknowledgement is
-- user-assigned metadata about a drive — the same kind of thing as its name, its tags and its bay —
-- and it should survive an unplug for exactly the same reason they do.
--
-- `ack_health` is the verdict that was acknowledged, and it is what makes the ack expire. The ack
-- covers the drive only while its verdict is *still that one*: a drive that gets worse is news, and
-- the user acknowledged the condition they could see rather than whatever happens next. That is a
-- better expiry than a timer, because it is exactly the condition the user named — and it means
-- there is no clock to get wrong.
--
-- `ack_health` empty means "not acknowledged", which is why it is NOT NULL DEFAULT '' rather than
-- nullable: there is no third state to distinguish here, since nothing about an ack is inferred
-- from the drive.

ALTER TABLE device_notes ADD COLUMN ack_health TEXT NOT NULL DEFAULT '';
ALTER TABLE device_notes ADD COLUMN ack_note TEXT NOT NULL DEFAULT '';
ALTER TABLE device_notes ADD COLUMN ack_at INTEGER NOT NULL DEFAULT 0;
