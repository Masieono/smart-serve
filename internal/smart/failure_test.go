package smart

import "testing"

// Every string below was observed on a real system rather than invented, in roughly this
// order of discovery. They look alike and need completely different fixes, which is the
// whole reason this classifier exists.
func TestClassifyFailureNamesTheGate(t *testing.T) {
	cases := []struct {
		name string
		text string
		want FailureCause
	}{
		{"read worked", "", CauseNone},

		// The container could not open the node at all — Docker's device cgroup, or
		// AppArmor, which returns the same errno.
		{"open refused", "Smartctl open device: /dev/nvme0n1 failed: Operation not permitted", CauseDeviceBlocked},
		{"open refused, other wording", "Smartctl open device: /dev/sda failed: Permission denied", CauseDeviceBlocked},
		{"node gone", "Smartctl open device: /dev/sdb failed: No such file or directory", CauseMissing},

		// The node opened and a query was refused — a missing capability.
		{"native nvme without SYS_ADMIN", "Read NVMe Identify Controller failed: Operation not permitted", CauseQueryBlocked},

		// The drive genuinely has nothing to say.
		{"usb flash drive", "SMART support is: Unavailable", CauseUnsupported},
		{"unknown bridge", "Unknown USB bridge [0x154b:0x6008 (0x100)]", CauseUnsupported},
		{"our own fallback", "no S.M.A.R.T. data reported", CauseUnsupported},

		{"unrecognised", "some new smartctl wording", CauseUnknown},
	}
	for _, c := range cases {
		if got := ClassifyFailure(c.text); got != c.want {
			t.Errorf("%s: ClassifyFailure(%q) = %q, want %q", c.name, c.text, got, c.want)
		}
	}
}

// A blocked read and an unsupported drive must never be conflated: one is the
// container's problem and the other is the drive's.
func TestBlockedIsNotUnsupported(t *testing.T) {
	for _, c := range []FailureCause{CauseDeviceBlocked, CauseQueryBlocked} {
		if !c.Blocked() {
			t.Errorf("%q should report Blocked", c)
		}
		if c.Hint() == "" {
			t.Errorf("%q should carry a fix for the operator", c)
		}
	}
	for _, c := range []FailureCause{CauseNone, CauseUnsupported, CauseMissing, CauseUnknown} {
		if c.Blocked() {
			t.Errorf("%q should not report Blocked", c)
		}
	}
}

// smartctl's casing is not a contract, and the text arrives from a JSON field we do not
// control.
func TestClassifyFailureIgnoresCaseAndPadding(t *testing.T) {
	for _, text := range []string{
		"  SMARTCTL OPEN DEVICE: /dev/sda FAILED: OPERATION NOT PERMITTED  ",
		"smartctl open device: /dev/sda failed: operation not permitted",
	} {
		if got := ClassifyFailure(text); got != CauseDeviceBlocked {
			t.Errorf("ClassifyFailure(%q) = %q, want %q", text, got, CauseDeviceBlocked)
		}
	}
}

func TestMessagesFromPrefersSmartctlsOwnWords(t *testing.T) {
	stdout := []byte(`{"messages":[{"string":"first"},{"string":"second"}]}`)
	if got := messagesFrom(stdout, []byte("stderr noise")); got != "first; second" {
		t.Errorf("messagesFrom = %q, want both messages joined", got)
	}
	// With no messages, stderr is all there is.
	if got := messagesFrom([]byte(`{}`), []byte("  cannot open  ")); got != "cannot open" {
		t.Errorf("messagesFrom = %q, want the trimmed stderr", got)
	}
	// Garbage must fall through rather than panic or produce junk.
	if got := messagesFrom([]byte("not json"), []byte("fallback")); got != "fallback" {
		t.Errorf("messagesFrom = %q, want the stderr fallback", got)
	}
	if got := messagesFrom(nil, nil); got != "" {
		t.Errorf("messagesFrom(nil, nil) = %q, want empty", got)
	}
}
