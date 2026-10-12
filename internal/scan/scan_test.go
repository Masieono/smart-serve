package scan

import (
	"context"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
)

// A Scanner must resolve its timeout and thresholds per scan, or a value changed in
// the UI would not take effect until the process restarted — which is exactly the
// bug that made the Settings page decorative.
func TestResolveUsesThePerScanFuncs(t *testing.T) {
	s := &Scanner{
		Timeout:    15 * time.Second,
		Thresholds: interp.DefaultThresholds(),
		TimeoutFunc: func(context.Context) time.Duration {
			return 42 * time.Second
		},
		ThresholdsFunc: func(context.Context) interp.Thresholds {
			tuned := interp.DefaultThresholds()
			tuned.PendingBad = 2
			return tuned
		},
	}

	timeout, thresholds := s.resolve(context.Background())
	if timeout != 42*time.Second {
		t.Errorf("timeout = %v, want the per-scan value", timeout)
	}
	if thresholds.PendingBad != 2 {
		t.Errorf("PendingBad = %d, want the per-scan value", thresholds.PendingBad)
	}
}

func TestResolveFallsBackToTheFields(t *testing.T) {
	s := New()

	timeout, thresholds := s.resolve(context.Background())
	if timeout != s.Timeout {
		t.Errorf("timeout = %v, want the field value %v", timeout, s.Timeout)
	}
	if thresholds != s.Thresholds {
		t.Errorf("thresholds = %+v, want the field value", thresholds)
	}
}

// A Func returning zero means "no opinion", not "no timeout".
func TestResolveIgnoresAZeroTimeout(t *testing.T) {
	s := &Scanner{
		Timeout:     15 * time.Second,
		Thresholds:  interp.DefaultThresholds(),
		TimeoutFunc: func(context.Context) time.Duration { return 0 },
	}

	timeout, _ := s.resolve(context.Background())
	if timeout != 15*time.Second {
		t.Errorf("timeout = %v, want the field value when the Func abstains", timeout)
	}
}
