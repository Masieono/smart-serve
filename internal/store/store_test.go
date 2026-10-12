package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesSchema(t *testing.T) {
	s := newTestStore(t)

	want := []string{
		"devices", "device_notes", "snapshots", "scan_history",
		"scan_schedules", "notification_channels", "notification_log",
		"settings", "device_transport_overrides", "schema_version",
	}
	for _, table := range want {
		var name string
		err := s.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", table, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "again.db")

	s1, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	var before int
	if err := s1.db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&before); err != nil {
		t.Fatalf("count schema_version: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if before == 0 {
		t.Fatal("no migrations were applied on first open")
	}

	// Re-opening must not re-run migrations or fail.
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	var after int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&after); err != nil {
		t.Fatalf("count schema_version: %v", err)
	}
	if after != before {
		t.Errorf("re-open re-ran migrations: %d -> %d", before, after)
	}
}

func TestSaveScanAndListDevices(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	started := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	ended := started.Add(5 * time.Second)
	capacity := uint64(1000204886016)
	temperature := 41
	hours := int64(1234)

	scanID, err := s.SaveScan(ctx, "manual", started, ended, []ScannedDevice{
		{
			Serial: "S1", Model: "Samsung SSD 870 EVO", DevicePath: "/dev/sda",
			Transport: "sata", SmartBus: "ata", CapacityBytes: &capacity,
			Mountpoints: []string{"/", "/boot"}, Fstype: "ext4",
			Health: "good", HealthRationale: "all checks passed", Score: 100,
			TemperatureC: &temperature, PowerOnHours: &hours, AttributesJSON: `{"5":0}`,
		},
		{
			Serial: "S2", Model: "WDC WD10EZEX", DevicePath: "/dev/sdb",
			Transport: "sata", SmartBus: "ata",
			Health: "caution", HealthRationale: "5 pending sectors", Score: 74,
		},
		{
			Serial: "S3", Model: "USB Bridge", DevicePath: "/dev/sdc", Transport: "usb",
			Health: "unknown", HealthRationale: "device open failed", Score: -1,
			Error: "error: device open failed",
		},
	})
	if err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	devices, err := s.ListDevices(ctx)
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("got %d devices, want 3", len(devices))
	}
	// Worst health first: caution, unknown, good.
	if devices[0].Serial != "S2" {
		t.Errorf("first device = %q, want S2 (caution)", devices[0].Serial)
	}
	if devices[2].Serial != "S1" {
		t.Errorf("last device = %q, want S1 (good)", devices[2].Serial)
	}

	good, err := s.GetDevice(ctx, "S1")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if good.Score != 100 || good.Health != "good" {
		t.Errorf("S1 = %+v", good)
	}
	if good.TemperatureC == nil || *good.TemperatureC != 41 {
		t.Errorf("S1 temperature = %v", good.TemperatureC)
	}
	if len(good.Mountpoints) != 2 || good.Mountpoints[0] != "/" || good.Fstype != "ext4" {
		t.Errorf("S1 mountpoints = %v, fstype = %q", good.Mountpoints, good.Fstype)
	}
	if good.LastScan == nil {
		t.Error("S1 last_scan should be set after a successful read")
	}
	if !good.SmartAvailable {
		t.Error("S1 smart_available should be true")
	}

	failed, err := s.GetDevice(ctx, "S3")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if failed.SmartAvailable {
		t.Error("S3 smart_available should be false")
	}
	if failed.LastScan != nil {
		t.Error("S3 last_scan must stay unset when the read failed")
	}

	history, err := s.ListScanHistory(ctx, 10)
	if err != nil {
		t.Fatalf("ListScanHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history rows = %d, want 1", len(history))
	}
	if history[0].ID != scanID {
		t.Errorf("scan id = %d, want %d", history[0].ID, scanID)
	}
	if history[0].DevicesTotal != 3 || history[0].DevicesOK != 2 || history[0].DevicesFailed != 1 {
		t.Errorf("counts = %+v, want total 3, ok 2, failed 1", history[0])
	}
}
