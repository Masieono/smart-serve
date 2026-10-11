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

// Scanner runs scans with a fixed timeout and threshold set.
type Scanner struct {
	Timeout    time.Duration
	Thresholds interp.Thresholds
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
	devices, err := smart.ListDevices(ctx, s.Timeout)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, s.scanOne(ctx, d))
	}
	return out, nil
}

func (s *Scanner) scanOne(ctx context.Context, d smart.Discovered) Device {
	res := Device{Discovered: d}

	args := []string{"-x", "--json"}
	if d.SmartType != "" {
		args = append(args, "-d", d.SmartType)
	}
	args = append(args, d.Path)

	cmdRes, err := smart.Run(ctx, s.Timeout, "smartctl", args...)
	res.Raw = cmdRes.Stdout
	if err != nil {
		res.Error = err.Error()
		res.Health = interp.Evaluate(nil, s.Thresholds)
		return res
	}

	sd, perr := smart.ParseSMART(cmdRes.Stdout)
	if perr != nil {
		res.Error = perr.Error()
		res.Health = interp.Evaluate(nil, s.Thresholds)
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
	res.Health = interp.Evaluate(sd, s.Thresholds)
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
