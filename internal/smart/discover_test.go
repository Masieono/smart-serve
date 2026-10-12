package smart

import "testing"

func TestParseLsblkKeepsWholeDisksOnly(t *testing.T) {
	devices, err := parseLsblk(loadFixture(t, "lsblk.json"))
	if err != nil {
		t.Fatalf("parseLsblk: %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("got %d devices, want 3 (partitions and loops excluded)", len(devices))
	}

	// A whole disk has no mountpoint of its own; the partitions carry them.
	sda := devices[0]
	if sda.Path != "/dev/sda" || sda.Transport != "sata" {
		t.Errorf("sda = %+v", sda)
	}
	if sda.Model != "BTO-1TB" {
		t.Errorf("model = %q, want trimmed %q", sda.Model, "BTO-1TB")
	}
	if len(sda.Mountpoints) != 1 || sda.Mountpoints[0] != "/mnt/data" {
		t.Errorf("sda mountpoints = %v, want [/mnt/data] from its partition", sda.Mountpoints)
	}
	if sda.Fstype != "ext4" {
		t.Errorf("sda fstype = %q, want ext4 from its partition", sda.Fstype)
	}

	nvme := devices[1]
	if nvme.Serial != "S4Y4NJ0R222807" {
		t.Errorf("serial = %q, want trimmed", nvme.Serial)
	}
	want := []string{"/boot/efi", "/"}
	if len(nvme.Mountpoints) != len(want) {
		t.Fatalf("nvme mountpoints = %v, want %v", nvme.Mountpoints, want)
	}
	for i := range want {
		if nvme.Mountpoints[i] != want[i] {
			t.Errorf("nvme mountpoints = %v, want %v", nvme.Mountpoints, want)
		}
	}
	if nvme.Fstype != "vfat, ext4" {
		t.Errorf("nvme fstype = %q, want %q", nvme.Fstype, "vfat, ext4")
	}

	// A disk with no partitions reports nothing rather than an empty string.
	usb := devices[2]
	if !usb.Removable || usb.Transport != "usb" || !usb.Rotational {
		t.Errorf("usb device flags wrong: %+v", usb)
	}
	if len(usb.Mountpoints) != 0 || usb.Fstype != "" {
		t.Errorf("usb mountpoints = %v, fstype = %q; want empty", usb.Mountpoints, usb.Fstype)
	}
}

func TestParseLsblkInvalid(t *testing.T) {
	if _, err := parseLsblk([]byte("{not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestCleanMountpoints(t *testing.T) {
	got := cleanMountpoints([]string{"", "/", "  ", "/mnt/x"})
	want := []string{"/", "/mnt/x"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
