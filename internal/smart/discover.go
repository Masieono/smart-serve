package smart

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Discovered is one candidate block device, merged from lsblk and smartctl.
type Discovered struct {
	Path        string   `json:"path"`
	KName       string   `json:"kname"`
	Model       string   `json:"model"`
	Serial      string   `json:"serial"`
	SizeBytes   uint64   `json:"size_bytes"`
	Transport   string   `json:"transport"`
	Fstype      string   `json:"fstype"`
	Mountpoints []string `json:"mountpoints"`
	Removable   bool     `json:"removable"`
	Rotational  bool     `json:"rotational"`
	WWN         string   `json:"wwn,omitempty"`
	// Rev and FsVersion come from lsblk alone. They are the only firmware and
	// filesystem detail available for a drive that reports no S.M.A.R.T.
	Rev       string `json:"rev,omitempty"`
	FsVersion string `json:"fs_version,omitempty"`
	SmartType string `json:"smart_type,omitempty"` // smartctl -d transport hint
}

type lsblkOut struct {
	Blockdevices []lsblkDev `json:"blockdevices"`
}

type lsblkDev struct {
	Name        string     `json:"name"`
	KName       string     `json:"kname"`
	Model       *string    `json:"model"`
	Serial      *string    `json:"serial"`
	Size        uint64     `json:"size"`
	Type        string     `json:"type"`
	Tran        *string    `json:"tran"`
	Fstype      *string    `json:"fstype"`
	Fsver       *string    `json:"fsver"`
	Rev         *string    `json:"rev"`
	Mountpoints []string   `json:"mountpoints"`
	Rota        bool       `json:"rota"`
	Rm          bool       `json:"rm"`
	WWN         *string    `json:"wwn"`
	Children    []lsblkDev `json:"children"`
}

type smartScanOut struct {
	Devices []struct {
		Name     string `json:"name"`
		InfoName string `json:"info_name"`
		Type     string `json:"type"`
		Protocol string `json:"protocol"`
	} `json:"devices"`
}

// ListDevices discovers block devices. lsblk is the source of truth for coverage
// ("if it shows up in lsblk it shows up in the UI"); smartctl --scan-open
// contributes the transport hint needed for some USB bridges.
func ListDevices(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
	lsblkRes, err := Run(ctx, timeout, "lsblk",
		"--json", "--paths", "--bytes",
		"--output", "NAME,KNAME,MODEL,SERIAL,SIZE,TYPE,TRAN,MOUNTPOINTS,FSTYPE,FSVER,REV,ROTA,RM,WWN",
	)
	if err != nil {
		return nil, fmt.Errorf("lsblk: %w", err)
	}
	devices, err := parseLsblk(lsblkRes.Stdout)
	if err != nil {
		return nil, err
	}

	if scanRes, serr := Run(ctx, timeout, "smartctl", "--scan-open", "--json"); serr == nil {
		var sc smartScanOut
		if json.Unmarshal(scanRes.Stdout, &sc) == nil {
			hints := make(map[string]string, len(sc.Devices))
			for _, d := range sc.Devices {
				hints[d.Name] = d.Type
			}
			for i := range devices {
				devices[i].SmartType = hints[devices[i].Path]
			}
		}
	}
	return devices, nil
}

// parseLsblk decodes lsblk JSON, keeping whole disks only. Partitions, loop
// devices, and other pseudo devices are dropped; a whole disk is the unit of
// S.M.A.R.T.
func parseLsblk(data []byte) ([]Discovered, error) {
	var lb lsblkOut
	if err := json.Unmarshal(data, &lb); err != nil {
		return nil, fmt.Errorf("parse lsblk json: %w", err)
	}
	out := make([]Discovered, 0, len(lb.Blockdevices))
	for _, d := range lb.Blockdevices {
		if d.Type != "disk" {
			continue
		}
		// A whole disk is never mounted itself — its partitions carry the
		// mountpoints and filesystems — so gather them from the tree.
		var mounts, fstypes, fsvers []string
		collectTree(d, &mounts, &fstypes, &fsvers)

		out = append(out, Discovered{
			Path:        d.Name,
			KName:       d.KName,
			Model:       deref(d.Model),
			Serial:      deref(d.Serial),
			SizeBytes:   d.Size,
			Transport:   deref(d.Tran),
			Fstype:      strings.Join(cleanMountpoints(fstypes), ", "),
			Mountpoints: cleanMountpoints(mounts),
			Removable:   d.Rm,
			Rotational:  d.Rota,
			WWN:         deref(d.WWN),
			Rev:         deref(d.Rev),
			FsVersion:   strings.Join(cleanMountpoints(fsvers), ", "),
		})
	}
	return out, nil
}

// collectTree walks a device and its children, gathering mountpoints, filesystems and
// filesystem versions (the disk's own values first, then its partitions').
func collectTree(d lsblkDev, mounts, fstypes, fsvers *[]string) {
	*mounts = append(*mounts, d.Mountpoints...)
	if d.Fstype != nil {
		*fstypes = append(*fstypes, *d.Fstype)
	}
	if d.Fsver != nil {
		*fsvers = append(*fsvers, *d.Fsver)
	}
	for _, child := range d.Children {
		collectTree(child, mounts, fstypes, fsvers)
	}
}

// cleanMountpoints trims, drops empties, and de-duplicates while preserving
// order.
func cleanMountpoints(in []string) []string {
	out := make([]string, 0, len(in))
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m == "" || contains(out, m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
