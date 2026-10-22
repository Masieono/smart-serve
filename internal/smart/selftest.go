package smart

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SelfTestKind is which self-test to ask a drive for.
type SelfTestKind string

const (
	// SelfTestShort checks the electronics and a sample of the media. Minutes.
	SelfTestShort SelfTestKind = "short"
	// SelfTestLong scans the whole surface, which is the only check that reads every sector.
	// Hours on a large drive, which is why it is never the default and never automatic.
	SelfTestLong SelfTestKind = "long"
)

// Valid reports whether a value names a test this app will run.
//
// The value reaches a command line, so anything unrecognised is refused rather than passed
// through. It is also the whole allow-list: these are the only two strings that can ever be
// handed to `smartctl -t`.
func (k SelfTestKind) Valid() bool {
	return k == SelfTestShort || k == SelfTestLong
}

// selfTestArgs builds the arguments for one self-test command.
//
// The per-device `-d` override is carried through exactly as the scan carries it, because a
// drive behind a USB bridge that needs `-d sat` needs it for this too — and a self-test is the
// worst place to discover that it was dropped.
func selfTestArgs(path, transport string, extra ...string) []string {
	args := append([]string{}, extra...)
	if transport != "" {
		args = append(args, "-d", transport)
	}
	return append(args, path)
}

// StartSelfTest asks a drive to run a self-test.
//
// This is the only thing in the app that **writes** to a drive, and it is why the feature is
// opt-in and never automatic (`FEATURES.md` A19, `ADR-017`). The command is the whole of it:
// the test itself runs inside the drive, which is what makes reading its progress afterwards
// both cheap and safe.
//
// Success is decided by the exit status, not by the text. The drive's acknowledgement is
// returned as a message for a caller to show, not parsed for meaning.
func StartSelfTest(ctx context.Context, timeout time.Duration, path, transport string, kind SelfTestKind) (Result, error) {
	if !kind.Valid() {
		return Result{}, fmt.Errorf("unknown self-test %q", kind)
	}
	res, err := Run(ctx, timeout, "smartctl", selfTestArgs(path, transport, "-t", string(kind), "--json")...)
	if err != nil {
		return res, err
	}
	return res, commandFailure(res)
}

// AbortSelfTest stops whichever test the drive is running.
//
// A long test on a large drive runs for hours, so someone who started one by mistake needs a way
// out that is not "wait". `-X` aborts a test of any kind, so there is nothing to look up first.
func AbortSelfTest(ctx context.Context, timeout time.Duration, path, transport string) (Result, error) {
	res, err := Run(ctx, timeout, "smartctl", selfTestArgs(path, transport, "-X", "--json")...)
	if err != nil {
		return res, err
	}
	return res, commandFailure(res)
}

// ReadSelfTestLog reads a drive's logs, for progress while a test runs.
//
// It uses the same `-x` invocation the scan uses rather than a narrower `-l selftest`, and that
// is a deliberate trade. The shape of `-x` is verified against three real drives; the log-only
// variant nests its answer differently, so a narrower command would mean a second parser to keep
// in step with the first, and a second chance to be wrong about a shape nobody has seen. The
// cost is a few extra log reads per poll, on a drive that is by definition already busy.
func ReadSelfTestLog(ctx context.Context, timeout time.Duration, path, transport string) (Result, error) {
	return Run(ctx, timeout, "smartctl", selfTestArgs(path, transport, "-x", "--json")...)
}

// commandFailure turns smartctl's exit status into an error, or nil.
//
// Only the low three bits mean the *command* did not work: an option it could not parse, a
// device it could not open, and a SMART command the drive refused. The other bits describe the
// drive itself — it is failing, it has a past failure, its logs hold errors — and those are the
// verdict engine's business. Treating them as a failed self-test would report a command that
// worked perfectly as broken, on exactly the drives that most need testing.
func commandFailure(res Result) error {
	if res.ExitCode&0x07 == 0 {
		return nil
	}
	// "exit status 4" is not a sentence, and smartctl prints nothing more often than you would
	// expect — a drive refusing a self-test can do it silently. So the bit is named in smartctl's
	// own vocabulary, and the absence of a reason is stated rather than left as a bare number the
	// reader has to go and look up.
	detail := exitMeaning(res.ExitCode & 0x07)
	if msg := messagesFrom(res.Stdout, res.Stderr); msg != "" {
		return fmt.Errorf("%s: %s", detail, msg)
	}
	return fmt.Errorf("%s, and it gave no reason (smartctl exit status %d)", detail, res.ExitCode)
}

// exitMeaning names the low three exit bits in smartctl's own words.
//
// Only the low three describe the *command*; the rest describe the drive, and this is reached only
// when one of the three is set. The wording is smartctl's man page rather than a paraphrase,
// because a paraphrase of a failure this app cannot diagnose would be an invented one.
func exitMeaning(bits int) string {
	var parts []string
	if bits&0x01 != 0 {
		parts = append(parts, "the command line did not parse")
	}
	if bits&0x02 != 0 {
		parts = append(parts, "the device could not be opened")
	}
	if bits&0x04 != 0 {
		parts = append(parts, "a SMART command failed")
	}
	if len(parts) == 0 {
		return "smartctl failed"
	}
	return "smartctl: " + strings.Join(parts, ", ")
}

// Messages returns smartctl's own words from a result, joined.
//
// Exported so the self-test endpoints can pass a drive's acknowledgement straight through
// instead of paraphrasing it: "Please wait 2 minutes for test to complete" is more useful than
// anything this app would write in its place.
func Messages(stdout, stderr []byte) string {
	return messagesFrom(stdout, stderr)
}
