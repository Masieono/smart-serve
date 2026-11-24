package scan

import (
	"testing"

	"github.com/masieono/smart-serve/internal/smart"
)

// Two identical enclosures, neither reporting a serial: the fallback identity has to differ, or the
// second drive upserts over the first and their histories interleave. A pair of docks becoming one
// drive is a silent loss, and `DECISIONS.md` promised the `-<n>` disambiguation the code never did.
func TestModelLessDrivesGetDistinctIdentities(t *testing.T) {
	mk := func() Device {
		return Device{Discovered: smart.Discovered{Model: "USB 3.0 SCSI Disk Device", Path: "/dev/sdb"}}
	}
	devices := uniqueModelLessSerials([]Device{mk(), mk(), mk()})

	got := map[string]bool{}
	for _, d := range devices {
		got[serialFor(d)] = true
	}
	if len(got) != 3 {
		t.Fatalf("identities = %v, want three distinct ones", got)
	}
	// The first keeps the plain form, so a single such drive keeps its history across upgrades.
	if !got["__nomodel:USB 3.0 SCSI Disk Device"] {
		t.Errorf("the first drive lost its plain identity: %v", got)
	}
}

// A drive with a real serial is never touched, however many there are.
func TestRealSerialsAreLeftAlone(t *testing.T) {
	devices := uniqueModelLessSerials([]Device{
		{Discovered: smart.Discovered{Serial: "AAA"}},
		{Discovered: smart.Discovered{Serial: "BBB"}},
	})
	if serialFor(devices[0]) != "AAA" || serialFor(devices[1]) != "BBB" {
		t.Errorf("serials were rewritten: %q %q", serialFor(devices[0]), serialFor(devices[1]))
	}
}
