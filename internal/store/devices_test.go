package store

import (
	"context"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/model"
)

// recent is the comparison window the dashboard uses, matching interp.WorsenWindow. The
// store takes it as a parameter rather than importing interp, so the two ends of the rule
// are pinned together by the caller.
const recent = 30 * 24 * time.Hour

// sectorScan writes one snapshot carrying a bad-sector total, `day` days after base.
func sectorScan(t *testing.T, s *Store, base time.Time, day int, serial string, sectors *int64) {
	t.Helper()
	at := base.AddDate(0, 0, day)
	if _, err := s.SaveScan(context.Background(), "manual", at, at.Add(time.Second), []ScannedDevice{
		{Serial: serial, Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
			Sectors: sectors},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
}

// The dashboard needs to know which drives are getting worse, and it needs it for every
// drive at once — one query, not one per card.
func TestSectorTrendReportsGrowthBetweenTheEnds(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)

	zero, four, five := int64(0), int64(4), int64(5)
	sectorScan(t, s, base, 0, "growing", &zero)
	sectorScan(t, s, base, 10, "growing", &five)
	sectorScan(t, s, base, 0, "stable", &five)
	sectorScan(t, s, base, 10, "stable", &five)
	sectorScan(t, s, base, 0, "once", &four)
	// A drive that reports no sector counts at all — every NVMe — must not appear.
	sectorScan(t, s, base, 0, "nvme", nil)
	sectorScan(t, s, base, 10, "nvme", nil)

	trend, err := s.SectorTrend(ctx, base.Add(-time.Hour), recent)
	if err != nil {
		t.Fatalf("SectorTrend: %v", err)
	}
	// Ten days is shorter than the comparison window, so this falls back to the oldest
	// reading rather than declining to answer: a fresh install shedding sectors has to be
	// told, even with less than a month of history.
	if got := trend["growing"]; got != 5 {
		t.Errorf("growing = %d, want 5", got)
	}
	// A stable count is a present zero, which is different from absent: it means "we
	// looked and nothing changed".
	if got, ok := trend["stable"]; !ok || got != 0 {
		t.Errorf("stable = %d (present=%v), want a present 0", got, ok)
	}
	// One snapshot is not a trend, and saying "flat" would be inventing history.
	if _, ok := trend["once"]; ok {
		t.Error("a device with a single snapshot must be absent, not reported as flat")
	}
	if _, ok := trend["nvme"]; ok {
		t.Error("a drive that reports no sector counts must be absent")
	}
}

// The warning has to clear itself, or it is not a warning. A drive that gained sectors and
// then stabilised is not getting worse any more, however much worse it is than it was five
// months ago.
func TestSectorTrendDecaysOnceGrowthStops(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)

	zero, seven, nine := int64(0), int64(7), int64(9)
	// Gained sectors between day 0 and day 40, then flat.
	sectorScan(t, s, base, 0, "stopped", &zero)
	sectorScan(t, s, base, 40, "stopped", &seven)
	sectorScan(t, s, base, 80, "stopped", &seven)
	// Still gaining.
	sectorScan(t, s, base, 0, "growing", &zero)
	sectorScan(t, s, base, 40, "growing", &seven)
	sectorScan(t, s, base, 80, "growing", &nine)

	trend, err := s.SectorTrend(ctx, base.Add(-time.Hour), recent)
	if err != nil {
		t.Fatalf("SectorTrend: %v", err)
	}
	if got := trend["stopped"]; got != 0 {
		t.Errorf("stopped = %d, want 0 — the badge must clear once growth stops", got)
	}
	if got := trend["growing"]; got != 2 {
		t.Errorf("growing = %d, want 2 — the growth since day 40, not since day 0", got)
	}
}

// The window has to be honoured, or a drive that was repaired a year ago would look like
// it is still getting worse.
func TestSectorTrendIgnoresRowsBeforeTheWindow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)

	high, low := int64(9), int64(2)
	if _, err := s.SaveScan(ctx, "manual", base.AddDate(0, 0, -400),
		base.AddDate(0, 0, -400).Add(time.Second), []ScannedDevice{
			{Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
				Sectors: &high},
		}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
	sectorScan(t, s, base, 0, "A", &low)

	trend, err := s.SectorTrend(ctx, base.AddDate(0, 0, -30), recent)
	if err != nil {
		t.Fatalf("SectorTrend: %v", err)
	}
	if _, ok := trend["A"]; ok {
		t.Error("the old row is outside the window and leaves no trend to report")
	}
}

// The trend and longevity code reads history through SnapshotSeries, which deliberately
// skips the heavy attributes blob — so `sectors` has to survive the round trip on its own.
// It did not: the column was selected and scanned and then dropped on the floor, and
// nothing noticed until an end-to-end run, because SectorTrend uses its own query.
func TestSnapshotSeriesCarriesSectors(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	zero, seven := int64(0), int64(7)
	for _, c := range []struct {
		day     int
		sectors *int64
	}{{0, &zero}, {30, &seven}, {31, nil}} {
		at := base.AddDate(0, 0, c.day)
		if _, err := s.SaveScan(ctx, "manual", at, at.Add(time.Second), []ScannedDevice{
			{Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
				Sectors: c.sectors},
		}); err != nil {
			t.Fatalf("SaveScan: %v", err)
		}
	}

	series, err := s.SnapshotSeries(ctx, "A", base.Add(-time.Hour), base.AddDate(0, 0, 60))
	if err != nil {
		t.Fatalf("SnapshotSeries: %v", err)
	}
	if len(series) != 3 {
		t.Fatalf("got %d snapshots, want 3", len(series))
	}
	if series[0].Sectors == nil || *series[0].Sectors != 0 {
		t.Errorf("first snapshot sectors = %v, want 0", series[0].Sectors)
	}
	if series[1].Sectors == nil || *series[1].Sectors != 7 {
		t.Errorf("second snapshot sectors = %v, want 7", series[1].Sectors)
	}
	// Absent stays absent: a drive that reports no sector counts must not read as zero.
	if series[2].Sectors != nil {
		t.Errorf("third snapshot sectors = %v, want nil", series[2].Sectors)
	}
}

func TestSaveScanUpdatesAndMarksMissing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	t0 := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	if _, err := s.SaveScan(ctx, "manual", t0, t0.Add(time.Second), []ScannedDevice{
		{Serial: "A", Model: "M", Health: "good", Score: 100},
		{Serial: "B", Model: "N", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("first SaveScan: %v", err)
	}
	// Second scan: A degrades, B is absent.
	if _, err := s.SaveScan(ctx, "schedule", t1, t1.Add(time.Second), []ScannedDevice{
		{Serial: "A", Model: "M", Health: "caution", HealthRationale: "5 pending sectors", Score: 74},
	}); err != nil {
		t.Fatalf("second SaveScan: %v", err)
	}

	a, err := s.GetDevice(ctx, "A")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if a.Health != "caution" || a.Score != 74 {
		t.Errorf("A = %+v, want caution/74", a)
	}
	if !a.FirstSeen.Equal(t0) {
		t.Errorf("first_seen = %v, want %v (must be preserved)", a.FirstSeen, t0)
	}
	if a.IsMissing {
		t.Error("A should not be missing")
	}

	b, err := s.GetDevice(ctx, "B")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if !b.IsMissing {
		t.Error("B should be marked missing")
	}
}

// The lsblk facts exist mainly for drives that report no S.M.A.R.T., so they have to
// survive a round trip — including the difference between "not removable" and "lsblk
// did not say", which is why removable is a pointer rather than a bool.
func TestSaveScanStoresLsblkFacts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	yes, no := true, false
	at := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "STICK", Model: "USB DISK 3.0", Removable: &yes, FsVersion: "1.0",
			Fstype: "exfat", Firmware: "PMAP", Health: "unknown", Score: -1,
			Error: "SMART support is: Unavailable"},
		{Serial: "FIXED", Model: "SSD", Removable: &no, Health: "good", Score: 100},
		{Serial: "OLD", Model: "scanned before 0004", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	stick, err := s.GetDevice(ctx, "STICK")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if stick.Removable == nil || !*stick.Removable {
		t.Errorf("removable = %v, want true", stick.Removable)
	}
	if stick.Fstype != "exfat" || stick.FsVersion != "1.0" {
		t.Errorf("filesystem = %q %q, want exfat 1.0", stick.Fstype, stick.FsVersion)
	}
	// lsblk's REV fills the firmware gap when smartctl cannot be read.
	if stick.Firmware != "PMAP" {
		t.Errorf("firmware = %q, want PMAP", stick.Firmware)
	}
	if stick.SmartAvailable {
		t.Error("a drive whose smartctl read failed must not be S.M.A.R.T.-available")
	}

	fixed, err := s.GetDevice(ctx, "FIXED")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if fixed.Removable == nil || *fixed.Removable {
		t.Errorf("removable = %v, want false", fixed.Removable)
	}

	// "Not reported" must stay distinguishable from a definite "no".
	old, err := s.GetDevice(ctx, "OLD")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if old.Removable != nil {
		t.Errorf("removable = %v, want nil when nobody said", *old.Removable)
	}
}

func TestSaveScanRecordsSnapshots(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	at := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "A", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	series, err := s.SnapshotSeries(ctx, "A", at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil {
		t.Fatalf("SnapshotSeries: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(series))
	}
	if series[0].Health != "good" || series[0].Score != 100 {
		t.Errorf("snapshot = %+v", series[0])
	}
	if !series[0].TS.Equal(at) {
		t.Errorf("snapshot ts = %v, want %v", series[0].TS, at)
	}
}

func TestNotesAndTransportOverride(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.SetNotes(ctx, Notes{
		Serial: "X", Name: "boot", Notes: "bought 2024-03", Tags: "critical,os", Hidden: true,
	}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
	n, err := s.GetNotes(ctx, "X")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if n == nil || n.Name != "boot" || n.Tags != "critical,os" || !n.Hidden {
		t.Errorf("notes = %+v", n)
	}

	// Update replaces fields.
	if err := s.SetNotes(ctx, Notes{Serial: "X", Name: "os-drive"}); err != nil {
		t.Fatalf("SetNotes update: %v", err)
	}
	n, _ = s.GetNotes(ctx, "X")
	if n.Name != "os-drive" || n.Tags != "" || n.Hidden {
		t.Errorf("notes after update = %+v", n)
	}

	// Unknown serial yields nil, not an error.
	missing, err := s.GetNotes(ctx, "nope")
	if err != nil {
		t.Fatalf("GetNotes unknown: %v", err)
	}
	if missing != nil {
		t.Errorf("notes for unknown serial = %+v, want nil", missing)
	}

	if err := s.SetTransportOverride(ctx, "X", "sat"); err != nil {
		t.Fatalf("SetTransportOverride: %v", err)
	}
	got, err := s.GetTransportOverride(ctx, "X")
	if err != nil || got != "sat" {
		t.Errorf("override = %q, %v; want sat", got, err)
	}
}

func TestPruneRemovesOldSnapshots(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	old := time.Now().AddDate(0, 0, -200)
	recent := time.Now()

	if _, err := s.SaveScan(ctx, "manual", old, old, []ScannedDevice{
		{Serial: "P", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("SaveScan old: %v", err)
	}
	if _, err := s.SaveScan(ctx, "manual", recent, recent, []ScannedDevice{
		{Serial: "P", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("SaveScan recent: %v", err)
	}

	deleted, _, err := s.Prune(ctx, 180, 30)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Errorf("pruned %d snapshots, want 1", deleted)
	}

	series, err := s.SnapshotSeries(ctx, "P", recent.AddDate(0, 0, -365), recent.Add(time.Hour))
	if err != nil {
		t.Fatalf("SnapshotSeries: %v", err)
	}
	if len(series) != 1 {
		t.Errorf("remaining snapshots = %d, want 1", len(series))
	}
}

// selfTestState reads one device's stored state, failing if the drive is unknown.
func selfTestState(t *testing.T, s *Store, serial string) DeviceState {
	t.Helper()
	all, err := s.DeviceStates(context.Background())
	if err != nil {
		t.Fatalf("DeviceStates: %v", err)
	}
	st, ok := all[serial]
	if !ok {
		t.Fatalf("no state for %q", serial)
	}
	return st
}

// B9 compares the newest finished self-test against the one recorded last time, so that value has
// to survive a restart — and a read that failed has to leave it alone. Without the second half, a
// single transient smartctl failure would turn the next successful scan into a "new result"
// notification for a test that finished days ago.
func TestSelfTestResultIsRecordedAndSurvivesAFailedRead(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	at := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	hours := int64(2116)
	passed := true

	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "S1", Health: "good", Score: 100, SelfTest: &model.SelfTestEntry{
			Type: "Extended offline", Hours: &hours, Passed: &passed,
			Status: "Completed without error",
		}},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	st := selfTestState(t, s, "S1")
	if !st.SelfTestRecorded {
		t.Fatal("SelfTestRecorded = false, but the log was read")
	}
	if st.SelfTest == nil {
		t.Fatal("SelfTest = nil, but a finished test was recorded")
	}
	if st.SelfTest.Type != "Extended offline" || st.SelfTest.Status != "Completed without error" {
		t.Errorf("SelfTest = %+v", st.SelfTest)
	}
	if st.SelfTest.Hours == nil || *st.SelfTest.Hours != 2116 {
		t.Errorf("Hours = %v, want 2116", st.SelfTest.Hours)
	}
	if st.SelfTest.Passed == nil || !*st.SelfTest.Passed {
		t.Errorf("Passed = %v, want true", st.SelfTest.Passed)
	}

	// The next scan cannot read the drive at all. The baseline must not be erased.
	if _, err := s.SaveScan(ctx, "manual", at.Add(time.Hour), at.Add(time.Hour), []ScannedDevice{
		{Serial: "S1", Health: "good", Score: 100, Error: "smartctl: device open failed"},
	}); err != nil {
		t.Fatalf("SaveScan after a failed read: %v", err)
	}
	st = selfTestState(t, s, "S1")
	if st.SelfTest == nil || st.SelfTest.Type != "Extended offline" {
		t.Fatalf("a failed read erased the baseline: %+v", st.SelfTest)
	}
}

// A log that was read and holds no finished test is a baseline; a row the migration has never
// touched is not. Collapsing the two is exactly what would make the first scan after an upgrade
// announce every result the drive already holds.
func TestAnEmptySelfTestLogIsARecordedBaseline(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	at := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)

	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "EMPTY", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
	st := selfTestState(t, s, "EMPTY")
	if !st.SelfTestRecorded {
		t.Error("an empty log must still count as recorded")
	}
	if st.SelfTest != nil {
		t.Errorf("SelfTest = %+v, want nil for an empty log", st.SelfTest)
	}

	// A row written before 0007 has null in those columns, which is a different finding.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO devices
		(serial, model, health, health_rationale, score, first_seen, last_seen, is_missing)
		VALUES ('OLD', 'm', 'good', '', 100, 0, 0, 0)`); err != nil {
		t.Fatalf("insert pre-migration row: %v", err)
	}
	st = selfTestState(t, s, "OLD")
	if st.SelfTestRecorded {
		t.Error("a row with null selftest_type must not claim a baseline")
	}
	if st.SelfTest != nil {
		t.Errorf("SelfTest = %+v, want nil", st.SelfTest)
	}
}

// The bay is the one field that answers "which drive do I pull?", so it has to survive the round
// trip through SQLite and come back on the record the dashboard actually renders from.
func TestBaySurvivesTheNotesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	at := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "S1", Model: "m", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
	if err := s.SetNotes(ctx, Notes{Serial: "S1", Name: "parity", Bay: "bay 4"}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	rec, err := s.GetDevice(ctx, "S1")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if rec.Bay != "bay 4" {
		t.Errorf("DeviceRecord.Bay = %q, want %q", rec.Bay, "bay 4")
	}

	n, err := s.GetNotes(ctx, "S1")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if n == nil || n.Bay != "bay 4" {
		t.Fatalf("Notes.Bay = %+v, want %q", n, "bay 4")
	}

	// Clearing it must read back as empty rather than as a missing column: the field is
	// NOT NULL DEFAULT '' precisely so there is no third state to interpret.
	if err := s.SetNotes(ctx, Notes{Serial: "S1", Name: "parity"}); err != nil {
		t.Fatalf("SetNotes (clearing): %v", err)
	}
	rec, err = s.GetDevice(ctx, "S1")
	if err != nil {
		t.Fatalf("GetDevice after clearing: %v", err)
	}
	if rec.Bay != "" {
		t.Errorf("Bay = %q after clearing, want empty", rec.Bay)
	}
}

// A device with no notes row at all must still come back with an empty bay rather than failing
// the scan — the LEFT JOIN is what makes that true, and the dashboard lists every drive.
func TestADeviceWithNoNotesHasAnEmptyBay(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	at := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "BARE", Model: "m", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
	rec, err := s.GetDevice(ctx, "BARE")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if rec.Bay != "" {
		t.Errorf("Bay = %q, want empty", rec.Bay)
	}
}
