// The partition schema (`FEATURES.md` A28).
//
// The app has always asked lsblk for the whole device tree and then flattened it — the walk existed
// only to collect mountpoints, and the sizes, labels, types and usage were paid for on every scan and
// never seen. These pin what is kept.

package smart

import (
	"os"
	"path/filepath"
	"testing"
)

func parseFixture(t *testing.T, name string) []Discovered {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	devices, err := parseLsblk(raw)
	if err != nil {
		t.Fatalf("parseLsblk: %v", err)
	}
	return devices
}

// The realistic case: GPT, an EFI partition, and an LVM volume group inside a second partition. The
// nesting is the part a flat list has to get right — the logical volume is a row in its own right,
// not something the partition above it explains away.
func TestPartitionSchemaFromARealisticDisk(t *testing.T) {
	devices := parseFixture(t, "lsblk-gpt-lvm.json")
	if len(devices) != 1 {
		t.Fatalf("got %d disks, want 1", len(devices))
	}
	sda := devices[0]

	if sda.PartitionTable != "gpt" {
		t.Errorf("partition table = %q, want gpt", sda.PartitionTable)
	}
	if len(sda.Partitions) != 3 {
		t.Fatalf("got %d partitions, want 3 (EFI, LVM member, logical volume): %+v",
			len(sda.Partitions), sda.Partitions)
	}

	efi := sda.Partitions[0]
	if efi.Name != "/dev/sda1" || efi.Type != "part" {
		t.Errorf("first row = %+v, want the EFI partition", efi)
	}
	if efi.Label != "EFI" || efi.PartLabel != "EFI System Partition" {
		t.Errorf("EFI labels = %q / %q", efi.Label, efi.PartLabel)
	}
	if efi.PartTypeName != "EFI System" {
		t.Errorf("EFI partition type = %q", efi.PartTypeName)
	}
	if efi.Fstype != "vfat" || efi.FsVersion != "FAT32" {
		t.Errorf("EFI filesystem = %q %q, want vfat FAT32", efi.Fstype, efi.FsVersion)
	}
	if len(efi.Mountpoints) != 1 || efi.Mountpoints[0] != "/boot/efi" {
		t.Errorf("EFI mountpoints = %v", efi.Mountpoints)
	}
	// Usage, which is the field that needs the mountpoint visible to the container.
	if efi.UsePercent != "8%" {
		t.Errorf("EFI use%% = %q, want 8%%", efi.UsePercent)
	}
	if efi.UsedBytes == nil || *efi.UsedBytes != 42949672 {
		t.Errorf("EFI used = %v", efi.UsedBytes)
	}
	if efi.AvailBytes == nil || *efi.AvailBytes != 493921240 {
		t.Errorf("EFI available = %v", efi.AvailBytes)
	}

	member := sda.Partitions[1]
	if member.PartTypeName != "Linux LVM" {
		t.Errorf("second row type = %q, want Linux LVM", member.PartTypeName)
	}
	// A partition holding a volume group has no filesystem of its own and is mounted nowhere, so
	// its usage is absent rather than zero.
	if member.UsePercent != "" || member.UsedBytes != nil {
		t.Errorf("LVM member carries usage (%q / %v) it cannot have", member.UsePercent, member.UsedBytes)
	}

	vol := sda.Partitions[2]
	if vol.Type != "lvm" || vol.Name != "/dev/mapper/tank-media" {
		t.Errorf("third row = %+v, want the logical volume", vol)
	}
	if vol.Label != "media" || vol.Fstype != "ext4" {
		t.Errorf("volume label/filesystem = %q / %q", vol.Label, vol.Fstype)
	}
	if vol.UsePercent != "47%" {
		t.Errorf("volume use%% = %q, want 47%%", vol.UsePercent)
	}
}

// Unallocated space is the question the schema exists to answer: a 1 TB drive holding a 500 MB
// partition and a 999 GB one has almost nothing left, and a drive with a 200 GB partition on 2 TB
// has most of itself unused.
func TestUnallocatedIsApproximateAndSaysSo(t *testing.T) {
	sda := parseFixture(t, "lsblk-gpt-lvm.json")[0]

	if sda.UnallocatedBytes == nil {
		t.Fatal("a disk with a partition table should report unallocated space")
	}
	// 1 TB disk less a 512 MB and a 999 GB partition.
	const want = 1000204886016 - 536870912 - 999000000000
	if *sda.UnallocatedBytes != want {
		t.Errorf("unallocated = %d, want %d", *sda.UnallocatedBytes, want)
	}
}

// Sector sizes and discard support, which are the two facts worth having and not worth putting in
// front of everybody: 512e versus 4Kn decides pool compatibility, and a zero discard granularity is
// how a USB bridge that drops TRIM shows itself.
func TestSectorSizesAndDiscardSupport(t *testing.T) {
	sda := parseFixture(t, "lsblk-gpt-lvm.json")[0]

	if sda.LogSectorSize != 512 || sda.PhySectorSize != 4096 {
		t.Errorf("sector sizes = %d / %d, want 512 logical on 4096 physical (512e)",
			sda.LogSectorSize, sda.PhySectorSize)
	}
	if !sda.DiscardSupport {
		t.Error("a 4 GiB discard granularity is support")
	}
}

// A drive with no partition table reports no rows and *no* unallocated figure, rather than claiming
// the whole disk is free — which would be a confident wrong answer about a disk nobody can read.
func TestADiskWithNoPartitionTableReportsNoSchema(t *testing.T) {
	raw := []byte(`{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":2000398934016,
		"fstype":"ext4","mountpoints":["/mnt/backup"],"rota":true,"rm":true,"ro":true,
		"state":"running","pttype":null,"children":[]}]}`)
	parsed, err := parseLsblk(raw)
	if err != nil {
		t.Fatalf("parseLsblk: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("got %d disks, want 1", len(parsed))
	}
	usb := parsed[0]

	if len(usb.Partitions) != 0 {
		t.Errorf("partitions = %+v, want none", usb.Partitions)
	}
	if usb.UnallocatedBytes != nil {
		t.Errorf("unallocated = %v, want nil for a disk with no table", *usb.UnallocatedBytes)
	}
	// A whole-disk filesystem still shows in the aggregated fields the device page already used.
	if usb.Fstype != "ext4" || len(usb.Mountpoints) != 1 {
		t.Errorf("whole-disk filesystem = %q %v", usb.Fstype, usb.Mountpoints)
	}
	// Read-only and state, which are the two flags an end user can act on.
	if usb.PartitionTable != "" {
		t.Errorf("partition table = %q, want empty", usb.PartitionTable)
	}
	if !usb.Removable {
		t.Error("a USB enclosure should be removable")
	}
}
