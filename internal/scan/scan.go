// Package scan orchestrates a full device scan: discovery, S.M.A.R.T. capture,
// and interpretation.
package scan

import (
	"context"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/smart"
)

// Device is the fully evaluated result for one drive.
type Device struct {
	Discovered smart.Discovered `json:"discovered"`
	Data       *model.SmartData `json:"data,omitempty"`
	Health     interp.Result    `json:"health"`
	Raw        []byte           `json:"-"`
	Error      string           `json:"error,omitempty"`
}

// Scanner runs scans. The timeout and the interpretation thresholds are resolved
// per scan when the corresponding Func is set, so a change made in the UI takes
// effect on the next scan rather than at the next restart (ADR-009).
type Scanner struct {
	Timeout    time.Duration
	Thresholds interp.Thresholds

	// TimeoutFunc, when set, supplies the per-command timeout for each scan.
	TimeoutFunc func(ctx context.Context) time.Duration
	// ThresholdsFunc, when set, supplies the interpretation thresholds for each
	// scan. Without it the shipped defaults would silently ignore the settings the
	// user edits in the UI.
	ThresholdsFunc func(ctx context.Context) interp.Thresholds
}

// New returns a Scanner with the shipped defaults.
func New() *Scanner {
	return &Scanner{
		Timeout:    smart.DefaultTimeout,
		Thresholds: interp.DefaultThresholds(),
	}
}

// Run performs one full scan. Devices are scanned sequentially, never in
// parallel, to avoid I/O contention (D10).
func (s *Scanner) Run(ctx context.Context) ([]Device, error) {
	timeout, thresholds := s.resolve(ctx)

	devices, err := smart.ListDevices(ctx, timeout)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, s.scanOne(ctx, d, timeout, thresholds))
	}
	return out, nil
}

// resolve returns the timeout and thresholds to use for one scan, consulting the
// per-scan Funcs when they are set. Keeping this separate from Run means the
// resolution can be tested without shelling out to lsblk and smartctl.
func (s *Scanner) resolve(ctx context.Context) (time.Duration, interp.Thresholds) {
	timeout := s.Timeout
	if s.TimeoutFunc != nil {
		if v := s.TimeoutFunc(ctx); v > 0 {
			timeout = v
		}
	}
	thresholds := s.Thresholds
	if s.ThresholdsFunc != nil {
		thresholds = s.ThresholdsFunc(ctx)
	}
	return timeout, thresholds
}

func (s *Scanner) scanOne(ctx context.Context, d smart.Discovered, timeout time.Duration, thresholds interp.Thresholds) Device {
	res := Device{Discovered: d}

	args := []string{"-x", "--json"}
	if d.SmartType != "" {
		args = append(args, "-d", d.SmartType)
	}
	args = append(args, d.Path)

	cmdRes, err := smart.Run(ctx, timeout, "smartctl", args...)
	res.Raw = cmdRes.Stdout
	if err != nil {
		res.Error = err.Error()
		res.Health = interp.Evaluate(nil, thresholds)
		return res
	}

	sd, perr := smart.ParseSMART(cmdRes.Stdout)
	if perr != nil {
		res.Error = perr.Error()
		res.Health = interp.Evaluate(nil, thresholds)
		return res
	}
	if sd.Bus == "" {
		sd.Bus = busFromTransport(d.Transport)
	}
	// A device that reported no health data at all could not be read; surface
	// smartctl's reason rather than pretending it is healthy.
	if len(sd.Attributes) == 0 && sd.NVMe == nil && sd.Passed == nil {
		if len(sd.Messages) > 0 {
			res.Error = sd.Messages[0]
		} else {
			res.Error = "no S.M.A.R.T. data reported"
		}
	}
	res.Data = sd
	res.Health = interp.Evaluate(sd, thresholds)
	return res
}

// busFromTransport maps an lsblk transport to a S.M.A.R.T. bus when smartctl did
// not report one.
func busFromTransport(t string) model.Bus {
	switch strings.ToLower(t) {
	case "nvme":
		return model.BusNVMe
	case "sata", "ata":
		return model.BusATA
	default:
		return ""
	}
}
