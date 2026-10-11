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

	sda := devices[0]
	if sda.Path != "/dev/sda" || sda.Transport != "sata" {
		t.Errorf("sda = %+v", sda)
	}
	if sda.Model != "BTO-1TB" {
		t.Errorf("model = %q, want trimmed %q", sda.Model, "BTO-1TB")
	}
	if len(sda.Mountpoints) != 0 {
		t.Errorf("mountpoints = %v, want empty (unmounted)", sda.Mountpoints)
	}

	nvme := devices[1]
	if nvme.Serial != "S4Y4NJ0R222807" {
		t.Errorf("serial = %q, want trimmed", nvme.Serial)
	}
	if len(nvme.Mountpoints) != 1 || nvme.Mountpoints[0] != "/" {
		t.Errorf("mountpoints = %v, want [/]", nvme.Mountpoints)
	}

	usb := devices[2]
	if !usb.Removable || usb.Transport != "usb" || !usb.Rotational {
		t.Errorf("usb device flags wrong: %+v", usb)
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
