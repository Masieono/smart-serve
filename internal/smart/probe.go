package smart

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ProbeResult is what one startup device-access check found.
type ProbeResult struct {
	// Devices is how many block devices were visible at all. Zero means the /dev mount
	// is missing, which is a different problem from being blocked.
	Devices int
	// Path is the device that was actually probed.
	Path string
	// Cause is why the probe read produced nothing; CauseNone means it worked.
	Cause FailureCause
	// Detail is smartctl's own words, for the log line.
	Detail string
}

// ProbeDeviceAccess checks whether this process can actually read S.M.A.R.T., and names
// the obstacle when it cannot.
//
// The three ways a container blocks device access all surface as the same "Operation not
// permitted", and the first scan is a poor place to discover that: by then the user is
// looking at an empty dashboard with no idea which of three compose lines is missing. One
// `smartctl -i` at startup turns that into a log line.
//
// It costs one short-lived command and runs once, so it does not touch the idle-cost
// invariant. Probing a single drive is enough: a permission gate applies to all of them,
// and a per-drive problem is the device page's job to explain.
func ProbeDeviceAccess(ctx context.Context, timeout time.Duration) (ProbeResult, error) {
	devices, err := ListDevices(ctx, timeout)
	if err != nil {
		return ProbeResult{}, err
	}
	out := ProbeResult{Devices: len(devices)}
	if len(devices) == 0 {
		return out, nil
	}

	out.Path = devices[0].Path
	res, err := Run(ctx, timeout, "smartctl", "-i", "--json", out.Path)
	if err != nil {
		return out, err
	}
	out.Detail = messagesFrom(res.Stdout, res.Stderr)
	out.Cause = ClassifyFailure(out.Detail)
	return out, nil
}

// messagesFrom joins smartctl's own messages, falling back to stderr when it emitted none.
//
// The nesting matters: smartctl puts them under `smartctl.messages`, not at the top level,
// which is the same shape parse.go reads. An earlier version of this looked at the top
// level, where nothing ever appears — so the probe found no messages, classified an empty
// string as "no failure", and reported a blocked container as healthy. An unread message
// list and an empty one are indistinguishable, which is what made it invisible.
func messagesFrom(stdout, stderr []byte) string {
	var parsed struct {
		Smartctl struct {
			Messages []struct {
				Severity string `json:"severity"`
				String   string `json:"string"`
			} `json:"messages"`
		} `json:"smartctl"`
	}
	if json.Unmarshal(stdout, &parsed) == nil {
		parts := make([]string, 0, len(parsed.Smartctl.Messages))
		for _, m := range parsed.Smartctl.Messages {
			s := strings.TrimSpace(m.String)
			if s == "" {
				continue
			}
			if sev := strings.ToLower(strings.TrimSpace(m.Severity)); sev == "error" || sev == "warning" {
				s = sev + ": " + s
			}
			parts = append(parts, s)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	return strings.TrimSpace(string(stderr))
}
