package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/store"
)

// `FEATURES.md` A28: the partition schema has to survive the scan that produced it.
//
// `lsblk` was always asked for the whole tree and the walk existed only to collect mountpoints, so
// the partitions, their labels and their usage were fetched on every scan and discarded. This is the
// test that says they now reach the page.
func TestTheDiskLayoutSurvivesAScanAndReachesTheDevicePayload(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	saveDeviceWithLayout(t, st)

	rec := do(t, srv, http.MethodGet, "/api/devices/SN-LAYOUT", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Device struct {
			DiskLayout struct {
				Table            string `json:"table"`
				UnallocatedBytes uint64 `json:"unallocated_bytes"`
				LogSectorSize    int    `json:"log_sector_size"`
				PhySectorSize    int    `json:"phy_sector_size"`
				DiscardSupport   bool   `json:"discard_support"`
				Partitions       []struct {
					Name        string   `json:"name"`
					Type        string   `json:"type"`
					SizeBytes   uint64   `json:"size_bytes"`
					Label       string   `json:"label"`
					PartType    string   `json:"part_type"`
					Mountpoints []string `json:"mountpoints"`
					UsePercent  string   `json:"use_percent"`
					UsedBytes   *uint64  `json:"used_bytes"`
				} `json:"partitions"`
			} `json:"disk_layout"`
		} `json:"device"`
	}
	decodeBody(t, rec, &resp)

	layout := resp.Device.DiskLayout
	if layout.Table != "gpt" {
		t.Errorf("table = %q, want gpt", layout.Table)
	}
	if layout.LogSectorSize != 512 || layout.PhySectorSize != 4096 {
		t.Errorf("sector sizes = %d / %d, want 512e", layout.LogSectorSize, layout.PhySectorSize)
	}
	if !layout.DiscardSupport {
		t.Error("discard support was lost")
	}
	if layout.UnallocatedBytes == 0 {
		t.Error("unallocated space was lost")
	}

	if len(layout.Partitions) != 2 {
		t.Fatalf("partitions = %+v, want 2", layout.Partitions)
	}
	media := layout.Partitions[1]
	if media.Name != "/dev/sdb2" || media.Label != "media" {
		t.Errorf("second partition = %+v", media)
	}
	if media.PartType != "Linux filesystem" {
		t.Errorf("partition type = %q", media.PartType)
	}
	if media.UsePercent != "47%" {
		t.Errorf("use%% = %q, want 47%%", media.UsePercent)
	}
	if media.UsedBytes == nil || *media.UsedBytes != 469060000000 {
		t.Errorf("used = %v", media.UsedBytes)
	}
}

// A drive whose layout was never read carries no field rather than an empty object — the same rule
// the score breakdown follows, and for the same reason: absent is how this payload says "nothing to
// report", and `omitempty` does not drop a literal null.
func TestADriveWithNoLayoutCarriesNoDiskLayout(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := st.SaveScan(ctx, "manual", now, now, []store.ScannedDevice{
		{Serial: "SN-BARE", Model: "USB Flash Disk", DevicePath: "/dev/sdc", Health: "unknown", Score: -1},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/devices/SN-BARE", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "disk_layout") {
		t.Errorf("a drive with no layout should carry no disk_layout field: %s", rec.Body.String())
	}
}

// saveDeviceWithLayout stores one drive with a partition table, in the shape the scan writes.
func saveDeviceWithLayout(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := st.SaveScan(ctx, "manual", now, now, []store.ScannedDevice{{
		Serial: "SN-LAYOUT", Model: "Samsung SSD 870 EVO", DevicePath: "/dev/sdb",
		Health: "good", Score: 96,
		DiskLayout: `{"table":"gpt","log_sector_size":512,"phy_sector_size":4096,` +
			`"discard_support":true,"unallocated_bytes":1074000000,` +
			`"partitions":[` +
			`{"name":"/dev/sdb1","type":"part","size_bytes":536870912,"fstype":"vfat",` +
			`"label":"EFI","part_type":"EFI System","mountpoints":["/boot/efi"],"use_percent":"8%"},` +
			`{"name":"/dev/sdb2","type":"part","size_bytes":999000000000,"fstype":"ext4",` +
			`"label":"media","part_type":"Linux filesystem","mountpoints":["/mnt/media"],` +
			`"use_percent":"47%","used_bytes":469060000000}]}`,
	}})
	if err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
}
