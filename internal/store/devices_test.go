package store

import (
	"context"
	"fmt"
	"strings"
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

// The in-progress self-test survives the round trip, and is *cleared* when one is no longer running
// (`FEATURES.md` A20).
//
// Cleared rather than merged is the whole invariant. The finished result beside it is COALESCEd, so a
// scan that could not read the log keeps the last known outcome — right for a result, wrong for a
// running test, where a stale badge would say a drive is busy long after it finished.
func TestSelfTestStateIsRecordedAndCleared(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	at := time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)

	running := 62
	if _, err := s.SaveScan(ctx, "manual", at, at.Add(time.Second), []ScannedDevice{{
		Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
		SelfTestRunning: true, SelfTestPct: &running, SelfTestKind: "Extended offline",
	}}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	rec := deviceNamed(t, s, "A")
	if !rec.SelfTestRunning {
		t.Error("a running test was not recorded")
	}
	if rec.SelfTestPct == nil || *rec.SelfTestPct != 62 {
		t.Errorf("pct = %v, want 62", rec.SelfTestPct)
	}
	if rec.SelfTestKind != "Extended offline" {
		t.Errorf("kind = %q, want the drive's own words for the test", rec.SelfTestKind)
	}

	// The test finished. The next scan says so, and the badge must go with it.
	after := at.Add(time.Hour)
	if _, err := s.SaveScan(ctx, "manual", after, after.Add(time.Second), []ScannedDevice{{
		Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
	}}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	rec = deviceNamed(t, s, "A")
	if rec.SelfTestRunning {
		t.Error("a finished test is still showing as running; the badge would never go away")
	}
	if rec.SelfTestPct != nil {
		t.Errorf("pct = %v, want nil once nothing is running", *rec.SelfTestPct)
	}
	if rec.SelfTestKind != "" {
		t.Errorf("kind = %q, want empty once nothing is running", rec.SelfTestKind)
	}
}

// A drive whose S.M.A.R.T. could not be read is not testing itself as far as this app can tell, and
// the state is cleared for it too — otherwise a drive that failed mid-test would keep its badge for
// as long as the failure lasted.
func TestAFailedReadClearsTheSelfTestState(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	at := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)

	running := 40
	if _, err := s.SaveScan(ctx, "manual", at, at.Add(time.Second), []ScannedDevice{{
		Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
		SelfTestRunning: true, SelfTestPct: &running, SelfTestKind: "Short offline",
	}}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	later := at.Add(time.Hour)
	if _, err := s.SaveScan(ctx, "manual", later, later.Add(time.Second), []ScannedDevice{{
		Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "unknown", Score: -1,
		Error: "smartctl: device not found",
	}}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	if rec := deviceNamed(t, s, "A"); rec.SelfTestRunning {
		t.Error("a drive that could not be read is still showing as testing itself")
	}
}

// The self-test poll interval is one setting with two readers — the browser card and the server-side
// watcher (`FEATURES.md` A20) — so the *server* has to read it, and it has to be as defensive as the
// settings page is permissive.
func TestSelfTestPollSecondsClampsToTheOfferedRange(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Nothing stored: the default, which is what the settings page shows.
	if got := s.SelfTestPollSeconds(ctx); got != 15*time.Second {
		t.Errorf("default = %v, want 15s", got)
	}

	for _, tc := range []struct {
		stored string
		want   time.Duration
		why    string
	}{
		{"5", 5 * time.Second, "a value in range is used as-is"},
		{"1", 2 * time.Second, "below the floor is raised: a hand-edited 1 would hammer a drive"},
		{"600", 30 * time.Second, "above the ceiling is lowered: it would freeze the badge"},
		{"0", 15 * time.Second, "zero is not a usable interval, so it falls back"},
		{"", 15 * time.Second, "empty falls back"},
		{"soon", 15 * time.Second, "unparseable falls back rather than becoming zero"},
	} {
		if err := s.SetSetting(ctx, "selftest_poll_seconds", tc.stored); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		if got := s.SelfTestPollSeconds(ctx); got != tc.want {
			t.Errorf("stored %q: got %v, want %v — %s", tc.stored, got, tc.want, tc.why)
		}
	}
}

// The watcher asks the store which drives to read, and it must not be told about a drive it cannot
// address or one that is not attached (`FEATURES.md` A20). A drive with no path has nothing to read,
// and a missing drive is not testing anything as far as this app can tell.
func TestTestingDevicesIsNarrowAndAddressable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	running := 40

	if _, err := s.SaveScan(ctx, "manual", at, at.Add(time.Second), []ScannedDevice{
		{Serial: "testing", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
			SelfTestRunning: true, SelfTestPct: &running, SelfTestKind: "Extended offline"},
		{Serial: "idle", Model: "m", DevicePath: "/dev/sdb", Health: "good", Score: 100},
		{Serial: "nowhere", Model: "m", Health: "good", Score: 100,
			SelfTestRunning: true, SelfTestKind: "Short offline"},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
	// The transport override travels with it: without it the read fails on exactly the drives most
	// likely to be mid-test.
	if err := s.SetTransportOverride(ctx, "testing", "sat,12"); err != nil {
		t.Fatalf("SetTransportOverride: %v", err)
	}

	got, err := s.TestingDevices(ctx)
	if err != nil {
		t.Fatalf("TestingDevices: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("TestingDevices = %+v, want only the addressable running drive", got)
	}
	if got[0].Serial != "testing" || got[0].Path != "/dev/sda" || got[0].TransportOverride != "sat,12" {
		t.Errorf("TestingDevices = %+v, want serial, path and override", got[0])
	}
}

/** The stored row for a serial, failing the test when it is missing. */
func deviceNamed(t *testing.T, s *Store, serial string) DeviceRecord {
	t.Helper()
	devices, err := s.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	for _, d := range devices {
		if d.Serial == serial {
			return d
		}
	}
	t.Fatalf("no device %q in the store", serial)
	return DeviceRecord{}
}

// `used_pct` has to survive the round trip for exactly the reason `sectors` did — it was added to a
// struct that three separate queries populate, and a column added to the SELECT but not the Scan (or
// the reverse) fails silently on the paths nobody happens to exercise.
//
// Zero is asserted on explicitly: a drive measured at 0% full and a drive nobody could measure are
// different facts, and `NULL` is the only thing that keeps them apart.
func TestSnapshotSeriesCarriesUsedPct(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	zero, ninety := 0, 90
	for _, c := range []struct {
		day     int
		usedPct *int
	}{{0, &zero}, {30, &ninety}, {31, nil}} {
		at := base.AddDate(0, 0, c.day)
		if _, err := s.SaveScan(ctx, "manual", at, at.Add(time.Second), []ScannedDevice{
			{Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
				UsedPct: c.usedPct},
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
	if series[0].UsedPct == nil || *series[0].UsedPct != 0 {
		t.Errorf("first snapshot used_pct = %v, want an explicit 0", series[0].UsedPct)
	}
	if series[1].UsedPct == nil || *series[1].UsedPct != 90 {
		t.Errorf("second snapshot used_pct = %v, want 90", series[1].UsedPct)
	}
	if series[2].UsedPct != nil {
		t.Errorf("third snapshot used_pct = %v, want nil for an unmeasured drive", *series[2].UsedPct)
	}
}

// The same round trip through the two queries that load the attributes blob. They are separate
// queries with separate column lists, so one can be right while the other is wrong.
func TestSnapshotReadersAgreeAboutUsedPct(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	at := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)
	forty := 40
	if _, err := s.SaveScan(ctx, "manual", at, at.Add(time.Second), []ScannedDevice{
		{Serial: "A", Model: "m", DevicePath: "/dev/sda", Health: "good", Score: 100,
			UsedPct: &forty, AttributesJSON: `{"attributes":[]}`},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	latest, err := s.LatestSnapshot(ctx, "A")
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if latest == nil || latest.UsedPct == nil || *latest.UsedPct != 40 {
		t.Errorf("LatestSnapshot used_pct = %v, want 40", latest)
	}

	recent, err := s.RecentSnapshots(ctx, "A", 5)
	if err != nil {
		t.Fatalf("RecentSnapshots: %v", err)
	}
	if len(recent) != 1 || recent[0].UsedPct == nil || *recent[0].UsedPct != 40 {
		t.Errorf("RecentSnapshots used_pct = %v, want 40", recent)
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

// The fleet table shows each drive's age per row, so the list endpoint needs power-on hours for
// every drive at once. "The latest" has to be exactly one row even when two snapshots share a
// millisecond, which is why this keys on the highest id rather than the highest timestamp.
func TestLatestSnapshotFactsPicksTheNewestPerDevice(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	at := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	early, late := int64(100), int64(200)
	wearEarly, wearLate := 1.0, 2.0

	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "A", Health: "good", Score: 100, PowerOnHours: &early, WearPct: &wearEarly},
		{Serial: "B", Health: "good", Score: 100, PowerOnHours: &late, WearPct: &wearLate},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
	// A second scan of A only, in the same millisecond. B's figures must be untouched.
	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "A", Health: "good", Score: 100, PowerOnHours: &late, WearPct: &wearLate},
	}); err != nil {
		t.Fatalf("SaveScan 2: %v", err)
	}

	got, err := s.LatestSnapshotFacts(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshotFacts: %v", err)
	}
	if f := got["A"]; f.PowerOnHours == nil || *f.PowerOnHours != 200 {
		t.Errorf("A hours = %v, want 200 from the newest snapshot", f.PowerOnHours)
	}
	if f := got["B"]; f.PowerOnHours == nil || *f.PowerOnHours != 200 {
		t.Errorf("B hours = %v, want 200", f.PowerOnHours)
	}

	// A drive whose snapshot reports nothing comes back present but empty, rather than with
	// zeroes: "no reading" and "zero hours" are different claims.
	if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{
		{Serial: "C", Health: "unknown", Score: -1},
	}); err != nil {
		t.Fatalf("SaveScan 3: %v", err)
	}
	got, err = s.LatestSnapshotFacts(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshotFacts 2: %v", err)
	}
	if f, ok := got["C"]; !ok || f.PowerOnHours != nil || f.WearPct != nil {
		t.Errorf("C = %+v, want present with no readings", f)
	}
}

// The diff needs two snapshots *with* their attributes blob, which is exactly what SnapshotSeries
// refuses to load. Newest first, and bounded by the limit.
func TestRecentSnapshotsLoadsTheAttributesBlob(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)

	for i := range 3 {
		at := base.Add(time.Duration(i) * time.Hour)
		if _, err := s.SaveScan(ctx, "manual", at, at, []ScannedDevice{{
			Serial: "S1", Health: "good", Score: 100,
			AttributesJSON: fmt.Sprintf(
				`{"attributes":[{"id":9,"name":"Power_On_Hours","raw_value":%d}]}`, i),
		}}); err != nil {
			t.Fatalf("SaveScan %d: %v", i, err)
		}
	}

	got, err := s.RecentSnapshots(ctx, "S1", 2)
	if err != nil {
		t.Fatalf("RecentSnapshots: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d snapshots, want 2", len(got))
	}
	if got[0].TS.Before(got[1].TS) {
		t.Error("snapshots are not newest first")
	}
	if got[0].Attributes == "" {
		t.Error("the attributes blob is missing, and the diff is the only reason to ask for it")
	}
	if !strings.Contains(got[0].Attributes, `"raw_value":2`) {
		t.Errorf("newest blob = %q, want the third scan's", got[0].Attributes)
	}

	// A limit of zero is not a request for everything.
	if empty, err := s.RecentSnapshots(ctx, "S1", 0); err != nil || len(empty) != 0 {
		t.Errorf("RecentSnapshots(limit 0) = %v, %v; want none", empty, err)
	}
}
