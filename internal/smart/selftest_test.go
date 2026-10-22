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

// A refusal has to be a sentence. smartctl prints nothing more than an exit status more often than
// you would expect — a drive can refuse a self-test silently — and "smartctl exit status 4" leaves
// the reader to go and look up what bit 2 means.
func TestSelfTestRefusalNamesTheExitBit(t *testing.T) {
	cases := map[int]string{
		1: "did not parse",
		2: "could not be opened",
		4: "a SMART command failed",
		5: "did not parse",
	}
	for status, want := range cases {
		err := commandFailure(Result{ExitCode: status})
		if err == nil {
			t.Fatalf("exit status %d: no error", status)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("exit status %d: error = %q, want it to mention %q", status, err, want)
		}
	}

	// The drive's own words win when there are any, because they are the diagnosis and this is only
	// a name for the bit.
	err := commandFailure(Result{ExitCode: 4, Stderr: []byte("Invalid Field in Command")})
	if err == nil || !strings.Contains(err.Error(), "Invalid Field in Command") {
		t.Errorf("error = %v, want smartctl's own words passed through", err)
	}
}

// The high bits describe the drive rather than the command, so a drive that is merely failing must
// not be reported as a broken self-test — the same rule `commandFailure` already followed.
func TestSelfTestIgnoresDriveConditionBits(t *testing.T) {
	for _, status := range []int{0, 8, 16, 32, 64, 8 | 16 | 32 | 64} {
		if err := commandFailure(Result{ExitCode: status}); err != nil {
			t.Errorf("exit status %d: error = %v, want nil", status, err)
		}
	}
}
