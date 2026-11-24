package smart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// DefaultTimeout bounds each external command (docs/DEPLOYMENT.md).
const DefaultTimeout = 15 * time.Second

// ErrToolMissing indicates the external tool is not installed in the image.
var ErrToolMissing = errors.New("external tool not found")

// Result holds the output of one command execution.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Run executes name with args under a timeout, capturing stdout and stderr.
//
// A non-zero exit status is reported through ExitCode and is NOT itself treated
// as an error: smartctl signals conditions through exit bits while still
// emitting valid JSON, so callers parse Stdout regardless.
func Run(ctx context.Context, timeout time.Duration, name string, args ...string) (Result, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	// Wait gives up on the output pipes after the deadline, rather than waiting for every process
	// that inherited them. The timeout above is only a promise if something keeps it: a child that
	// forked, or one that ignores the signal, otherwise holds `Run` open past it.
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}

	// The context is checked first, and the order is the whole point: a process killed by the
	// deadline reports an *exec.ExitError like any other non-zero exit, so testing that first turned
	// every timeout into "exited with -1" and returned no error at all. A hung smartctl then looked
	// like a smartctl that ran and said nothing, and every caller's error branch stayed dead: a scan
	// blamed the truncated JSON, the self-test watcher cleared the fleet badge mid-test, and the
	// self-test runner started the next drive on top of one that was still testing.
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return res, fmt.Errorf("%s: timed out after %s", name, timeout)
		case errors.Is(ctx.Err(), context.Canceled):
			return res, fmt.Errorf("%s: cancelled", name)
		case errors.Is(err, exec.ErrNotFound):
			return res, ErrToolMissing
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	res.ExitCode = 0
	return res, nil
}
