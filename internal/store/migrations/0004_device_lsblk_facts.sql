-- 0004_device_lsblk_facts.sql — persist the facts lsblk knows and smartctl does not.
--
-- These matter most for the drives that have no S.M.A.R.T. at all — USB flash drives
-- and card readers — where lsblk is the only source of anything: whether the media is
-- removable, the filesystem version (exFAT, NTFS…), and the firmware revision lsblk
-- reports as REV, which fills the gap when smartctl cannot be read.
-- Kept as a separate migration so the released 0001-0003 stay immutable.

-- Nullable on purpose: "not removable" and "lsblk did not say" are different answers.
ALTER TABLE devices ADD COLUMN removable INTEGER;
ALTER TABLE devices ADD COLUMN fs_version TEXT NOT NULL DEFAULT '';