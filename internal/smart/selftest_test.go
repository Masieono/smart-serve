package smart

import (
	"strings"
	"testing"
)

// Only two strings may ever reach `smartctl -t`, and this is that list.
func TestSelfTestKindRejectsAnythingElse(t *testing.T) {
	for _, k := range []SelfTestKind{SelfTestShort, SelfTestLong} {
		if !k.Valid() {
			t.Errorf("%q should be valid", k)
		}
	}
	for _, k := range []SelfTestKind{"", "offline", "conveyance", "SHORT", "short; rm -rf /"} {
		if k.Valid() {
			t.Errorf("%q should be refused — the value reaches a command line", k)
		}
	}
}

// The per-device `-d` override has to survive. A drive behind a bridge that needs `-d sat` needs
// it for a self-test too, and this is the worst place to discover it was dropped.
func TestSelfTestArgsCarryTheTransportOverride(t *testing.T) {
	got := selfTestArgs("/dev/sda", "sat", "-t", "short", "--json")
	want := []string{"-t", "short", "--json", "-d", "sat", "/dev/sda"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", got, want)
	}

	// No override means no `-d` at all, rather than an empty one.
	got = selfTestArgs("/dev/sda", "", "-X", "--json")
	want = []string{"-X", "--json", "/dev/sda"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", got, want)
	}
}

// smartctl signals conditions through exit bits, and only the low three mean the *command* did
// not work. The rest describe the drive. Reading the drive's bits as a command failure would
// report a self-test that ran perfectly as broken, on exactly the drives that most need one.
func TestCommandFailureCountsOnlyTheLowThreeBits(t *testing.T) {
	cases := []struct {
		status int
		want   bool // true when this should be an error
	}{
		{0, false},
		{1, true},    // a command line it could not parse
		{2, true},    // a device it could not open
		{4, true},    // a SMART command the drive refused
		{8, false},   // DISK FAILING — the drive's problem, not the command's
		{16, false},  // a prefail attribute at or below threshold
		{32, false},  // a failure in the past
		{64, false},  // the error log holds errors
		{128, false}, // the self-test log holds errors
		{8 | 64, false},
		{1 | 8, true},
	}
	for _, c := range cases {
		err := commandFailure(Result{ExitCode: c.status})
		if (err != nil) != c.want {
			t.Errorf("exit status %d: error = %v, want error = %v", c.status, err, c.want)
		}
	}
}
