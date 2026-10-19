-- 0008_device_bay.sql — where the drive physically is.
--
-- A serial identifies a drive; a bay *finds* it. That distinction is the whole of D10: at 3am,
-- standing in front of a machine, "bay 4" is the answer you need and the serial is not.
--
-- User metadata, so it lives with the name and the tags rather than on `devices`: the row it
-- describes is the drive, and a bay is something a person assigned to it. It survives an unplug
-- for the same reason the notes do.
--
-- NOT NULL DEFAULT '' rather than nullable, unlike the self-test columns (0007). There is no
-- third state here to distinguish: nothing about a bay is inferred from the drive, so "no bay
-- recorded" and "the bay field is empty" are the same finding.

ALTER TABLE device_notes ADD COLUMN bay TEXT NOT NULL DEFAULT '';
