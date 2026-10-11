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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}

	var ee *exec.ExitError
	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
	case errors.Is(err, exec.ErrNotFound):
		return res, ErrToolMissing
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return res, fmt.Errorf("%s: timed out after %s", name, timeout)
	default:
		return res, err
	}
	return res, nil
}
