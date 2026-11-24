package smart

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A command that outlives its timeout must come back as an error.
//
// It used to come back as success with exit code -1, because the process a deadline kills reports an
// *exec.ExitError like any other non-zero exit and the switch tested that first. The difference
// matters more than it looks: every caller checks the error before it parses, so a swallowed timeout
// meant a hung drive was reported as a drive with unreadable JSON, a self-test badge was cleared
// mid-test, and the self-test runner moved on to the next drive while the last one was still testing.
func TestTimeoutIsAnErrorNotAnExitCode(t *testing.T) {
	res, err := Run(context.Background(), 150*time.Millisecond, "sh", "-c", "exec sleep 5")
	if err == nil {
		t.Fatalf("a command killed by its deadline was reported as success (exit %d, %d bytes)", res.ExitCode, len(res.Stdout))
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %q, want a message that says it timed out", err)
	}
}

// The contract the callers rely on: a non-zero exit is data, not an error, because smartctl signals
// conditions through its exit bits while still emitting valid JSON.
func TestNonZeroExitIsNotAnError(t *testing.T) {
	res, err := Run(context.Background(), 5*time.Second, "sh", "-c", "printf hi; exit 3")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", res.ExitCode)
	}
	if string(res.Stdout) != "hi" {
		t.Errorf("stdout = %q, want %q", res.Stdout, "hi")
	}
}

// Cancellation is not a timeout and the message should say which happened: one means "this drive is
// not answering", the other means "we stopped asking".
func TestCancellationIsReportedAsSuch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	_, err := Run(ctx, 5*time.Second, "sh", "-c", "exec sleep 5")
	if err == nil {
		t.Fatal("a cancelled command was reported as success")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("err = %q, want it to say cancelled", err)
	}
}

// A missing binary is its own error, still, so the caller can say "smartctl is not installed" rather
// than "the drive failed".
func TestMissingToolIsReportedAsSuch(t *testing.T) {
	_, err := Run(context.Background(), time.Second, "smart-serve-no-such-binary")
	if err == nil {
		t.Fatal("a missing binary was reported as success")
	}
	if !strings.Contains(err.Error(), "not found") && err != ErrToolMissing {
		t.Errorf("err = %v, want ErrToolMissing", err)
	}
}
