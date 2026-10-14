package store

import (
	"context"
	"testing"
	"time"
)

// The dashboard needs to know which drives are getting worse, and it needs it for every
// drive at once — one query, not one per card.
func TestSectorTrendReportsGrowthBetweenTheEnds(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	save := func(day int, serial string, sectors *int64) {
		t.Helper()
		at := base.AddDate(0, 0, day)
		if _, err := s.SaveScan(ctx, "manual", at, at.Add(time.Second), []ScannedDevice{
			{Serial: serial, Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
				Sectors: sectors},
		}); err != nil {
			t.Fatalf("SaveScan: %v", err)
		}
	}

	zero, four, five := int64(0), int64(4), int64(5)
	save(0, "growing", &zero)
	save(10, "growing", &five)
	save(0, "stable", &five)
	save(10, "stable", &five)
	save(0, "once", &four)
	// A drive that reports no sector counts at all — every NVMe — must not appear.
	save(0, "nvme", nil)
	save(10, "nvme", nil)

	trend, err := s.SectorTrend(ctx, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("SectorTrend: %v", err)
	}
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

// The window has to be honoured, or a drive that was repaired a year ago would look like
// it is still getting worse.
func TestSectorTrendIgnoresRowsBeforeTheWindow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	high, low := int64(9), int64(2)
	for _, c := range []struct {
		at      time.Time
		sectors *int64
	}{{base.AddDate(0, 0, -400), &high}, {base, &low}} {
		if _, err := s.SaveScan(ctx, "manual", c.at, c.at.Add(time.Second), []ScannedDevice{
			{Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
				Sectors: c.sectors},
		}); err != nil {
			t.Fatalf("SaveScan: %v", err)
		}
	}

	trend, err := s.SectorTrend(ctx, base.AddDate(0, 0, -30))
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
