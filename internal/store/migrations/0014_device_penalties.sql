-- 0014_device_penalties.sql — why the score is what it is.
--
-- `FEATURES.md` A12. The interpreter has always produced a per-drive breakdown of the 0-100 score
-- (`interp.Penalty`: a reason and a number of points), and the scan has always thrown it away — it
-- was computed on every read and never stored, so the one question a score invites ("why is it
-- 72?") had an answer that existed for a few milliseconds and was discarded.
--
-- Stored on `devices` rather than on `snapshots`, beside `health_rationale` and `score`, because it
-- explains the *current* verdict — the same reason the rationale lives there. A snapshot's score has
-- its own breakdown, and nothing asks for it: the trend chart plots the score, not the reasons.
--
-- A JSON array, like `mountpoints` and for the same reason: it is a small opaque list that is read
-- whole and never queried by. Each entry carries an optional `attribute_id`, so the attribute table
-- can mark the rows the verdict actually read rather than inferring it from thresholds.
--
-- DEFAULT '' rather than '[]' so an existing row is distinguishable from a drive with a clean
-- breakdown — the two are both "no lines", but only one of them has been computed.

ALTER TABLE devices ADD COLUMN penalties TEXT NOT NULL DEFAULT '';
