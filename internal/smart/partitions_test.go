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

// The fullest filesystem, not an average (`FEATURES.md` A28).
//
// A drive holding a 20%-full boot partition and a 95%-full data volume is a drive about to run out of
// space. A blended figure would report it as half full and hide the volume somebody needs to be told
// about, which is the whole reason this picks rather than averages.
func TestFullestPicksTheFilesystemWithLeastRoom(t *testing.T) {
	layout := DiskLayout{Partitions: []Partition{
		{Name: "/dev/sda1", UsePercent: "8%"},
		{Name: "/dev/sda2", UsePercent: "95%"},
		{Name: "/dev/sda3", UsePercent: "47%"},
	}}

	got, ok := layout.Fullest()
	if !ok {
		t.Fatal("a layout with three measured filesystems reported none")
	}
	if got.Name != "/dev/sda2" {
		t.Errorf("Fullest = %q, want the 95%% filesystem", got.Name)
	}
}

// A partition with no figure is skipped rather than treated as zero — otherwise an unmounted volume
// would drag the answer down to the wrong filesystem.
func TestFullestIgnoresFilesystemsWithNoFigure(t *testing.T) {
	layout := DiskLayout{Partitions: []Partition{
		{Name: "/dev/sda1"},
		{Name: "/dev/sda2", UsePercent: "12%"},
		{Name: "/dev/sda3", UsePercent: ""},
	}}

	got, ok := layout.Fullest()
	if !ok {
		t.Fatal("a layout with one measured filesystem reported none")
	}
	if got.Name != "/dev/sda2" {
		t.Errorf("Fullest = %q, want the measured 12%% filesystem", got.Name)
	}
}

// "Nothing measured" and "measured, and empty" are different answers, and the caller has to be able
// to tell them apart — the list payload omits the storage block for the first and sends 0 for the
// second.
func TestFullestReportsNothingWhenNothingWasMeasured(t *testing.T) {
	for name, layout := range map[string]DiskLayout{
		"no partitions":      {},
		"none with a figure": {Partitions: []Partition{{Name: "/dev/sda1"}, {Name: "/dev/sda2"}}},
	} {
		if got, ok := layout.Fullest(); ok {
			t.Errorf("%s: Fullest reported %q, want nothing", name, got.Name)
		}
	}
}

// lsblk formats the percentage, so it arrives as a string with a sign on it. Junk must be refused
// rather than coerced, because `strconv` would otherwise turn some of it into a plausible number.
func TestUsePercentValueParsesLsblksFormat(t *testing.T) {
	for input, want := range map[string]int{
		"0%":   0,
		"47%":  47,
		"100%": 100,
		" 47%": 47,
	} {
		got, ok := Partition{UsePercent: input}.UsePercentValue()
		if !ok || got != want {
			t.Errorf("UsePercentValue(%q) = %d, %v; want %d, true", input, got, ok, want)
		}
	}
	for _, input := range []string{"", "  ", "%", "unknown", "-5%", "4.7%"} {
		if got, ok := (Partition{UsePercent: input}).UsePercentValue(); ok {
			t.Errorf("UsePercentValue(%q) = %d, true; want false", input, got)
		}
	}
}

// A disk pulled from a RAID array (`FEATURES.md` A28).
//
// This is the case that produced "either unpartitioned or its table could not be read" for a drive
// that was working perfectly. An md member has no partition table *because* it is an array member —
// the layout lives in the array — and lsblk reports exactly that as the disk node's own filesystem
// type. Saying so is the difference between a message that reads like a fault and one that describes
// a drive doing its job.
func TestARaidMemberIsNamedRatherThanCalledUnpartitioned(t *testing.T) {
	sdc := parseFixture(t, "lsblk-raid-member.json")[0]

	if len(sdc.Partitions) != 0 {
		t.Fatalf("a RAID member has no partitions, got %d", len(sdc.Partitions))
	}
	if sdc.PartitionTable != "" {
		t.Errorf("partition table = %q, want none", sdc.PartitionTable)
	}

	layout := sdc.DiskLayout()
	if layout == nil {
		t.Fatal("the layout was dropped; the card would not render at all")
	}
	if layout.WholeDiskFstype != "linux_raid_member" {
		t.Errorf("whole-disk signature = %q, want linux_raid_member", layout.WholeDiskFstype)
	}
	if layout.WholeDiskName != "a Linux md array" {
		t.Errorf("whole-disk name = %q, want the plain-language name", layout.WholeDiskName)
	}
}

// A partitioned disk's own signature is its *table*, so the whole-disk fields stay empty — otherwise
// the page would say "this disk holds a gpt filesystem".
func TestAPartitionedDiskReportsNoWholeDiskSignature(t *testing.T) {
	sda := parseFixture(t, "lsblk-gpt-lvm.json")[0]

	layout := sda.DiskLayout()
	if layout == nil {
		t.Fatal("no layout")
	}
	if layout.WholeDiskFstype != "" || layout.WholeDiskName != "" {
		t.Errorf("a partitioned disk reported a whole-disk signature: %q / %q",
			layout.WholeDiskFstype, layout.WholeDiskName)
	}
}

// The map is the whole mechanism, so it is worth pinning in both directions: a known token gets its
// name, and anything else gets nothing rather than a guess. `ext4` written straight to a disk is a
// filesystem, not a member of anything.
func TestWholeDiskMemberNames(t *testing.T) {
	for signature, want := range map[string]string{
		"linux_raid_member": "a Linux md array",
		"ISW_RAID_MEMBER":   "an Intel Rapid Storage array",
		" ddf_raid_member ": "a DDF array",
		"zfs_member":        "a ZFS pool",
		"LVM2_member":       "an LVM volume group",
		"crypto_LUKS":       "an encrypted volume",
	} {
		if got := WholeDiskMemberName(signature); got != want {
			t.Errorf("WholeDiskMemberName(%q) = %q, want %q", signature, got, want)
		}
	}

	// Not members: a plain filesystem, and nonsense.
	for _, signature := range []string{"", "  ", "ext4", "xfs", "btrfs", "ntfs", "not_a_thing"} {
		if got := WholeDiskMemberName(signature); got != "" {
			t.Errorf("WholeDiskMemberName(%q) = %q, want empty", signature, got)
		}
	}
}

// UUIDs are carried for the copy button rather than for display (`FEATURES.md` A28): they are what
// `/etc/fstab` speaks in, and nobody reads them. Both kinds are kept, and either can be absent —
// a filesystem UUID is the filesystem's, a partition UUID is the table's, and a partition with no
// filesystem has only the second.
func TestUUIDsAreKeptForTheCopyButton(t *testing.T) {
	sda := parseFixture(t, "lsblk-gpt-lvm.json")[0]

	efi := sda.Partitions[0]
	if efi.UUID != "1A2B-3C4D" {
		t.Errorf("EFI filesystem UUID = %q", efi.UUID)
	}
	if efi.PartUUID != "6f8b1c2d-3e4f-4a5b-8c9d-0e1f2a3b4c5d" {
		t.Errorf("EFI partition UUID = %q", efi.PartUUID)
	}

	// A partition holding a volume group has no filesystem, so no filesystem UUID — but it still
	// has a partition UUID, which is the one somebody would put in fstab's `PARTUUID=`.
	member := sda.Partitions[1]
	if member.UUID != "" {
		t.Errorf("LVM member filesystem UUID = %q, want none", member.UUID)
	}
	if member.PartUUID == "" {
		t.Error("the LVM member's partition UUID was lost")
	}

	// And the logical volume has the opposite: a filesystem, no partition table entry.
	vol := sda.Partitions[2]
	if vol.UUID == "" {
		t.Error("the logical volume's filesystem UUID was lost")
	}
	if vol.PartUUID != "" {
		t.Errorf("logical volume partition UUID = %q, want none", vol.PartUUID)
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
