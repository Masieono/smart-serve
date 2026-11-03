package smart

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

	// Partitions is the drive's partition schema (`FEATURES.md` A28), flat and in tree order. Empty
	// for a drive with no partition table — which is a fact worth reporting rather than a failure:
	// an unpartitioned disk is a disk nobody has set up yet.
	Partitions []Partition `json:"partitions,omitempty"`
	// UnallocatedBytes is disk size less the sum of its partitions, and it is *approximate*: it
	// ignores alignment gaps, GPT's backup header and any nested volume that does not report a size.
	// It answers "is this drive mostly empty?", which is a question about hundreds of gigabytes —
	// and it is nil when there is no partition table to reason about, rather than claiming the whole
	// disk is unallocated.
	UnallocatedBytes *uint64 `json:"unallocated_bytes,omitempty"`
	// PartitionTable is lsblk's PTTYPE (gpt, dos). Kept because it is what tells "no partition
	// table" apart from "a partition table lsblk could not read".
	PartitionTable string `json:"partition_table,omitempty"`
	// LogSectorSize and PhySectorSize are the 512e / 4Kn distinction, which decides whether a drive
	// can join a pool that demands one. Power-user facts: shown behind a disclosure, not in the
	// table (`A28`).
	LogSectorSize int `json:"log_sector_size,omitempty"`
	PhySectorSize int `json:"phy_sector_size,omitempty"`
	// DiscardSupport is whether the device advertises TRIM. False on an SSD behind a bridge that
	// does not pass it through, which is a real finding for a drive in an enclosure.
	DiscardSupport bool `json:"discard_support,omitempty"`
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

	// The partition schema (`FEATURES.md` A28). Every one of these is on the wire already — the
	// tree was walked only to collect mountpoints and then discarded, so the sizes, labels and
	// usage were paid for on every scan and never seen.
	Label        *string `json:"label"`
	PartLabel    *string `json:"partlabel"`
	PartTypeName *string `json:"parttypename"`
	PartN        *int    `json:"partn"`
	State        *string `json:"state"`
	Ro           bool    `json:"ro"`
	// Usage. Populated only where the container can see the mountpoint: lsblk reads this with
	// statvfs(2) on the mount path, so a filesystem it cannot reach reports nothing rather than
	// zero. `Fsuse` arrives as a string because lsblk formats it ("47%").
	Fsuse   *string `json:"fsuse%"`
	FsUsed  *uint64 `json:"fsused"`
	FsSize  *uint64 `json:"fssize"`
	FsAvail *uint64 `json:"fsavail"`
	// Sector sizes and discard support. Power-user facts, shown behind a disclosure rather than in
	// the table.
	LogSec   *int    `json:"log-sec"`
	PhySec   *int    `json:"phy-sec"`
	DiscGran *uint64 `json:"disc-gran"`
	DiscMax  *uint64 `json:"disc-max"`
	DiscZero *bool   `json:"disc-zero"`
	// PtType is the partition table type (gpt, dos). Not shown itself — it is what makes
	// PartTypeName meaningful, and it is how a disk with no table is told from one whose table
	// lsblk could not read.
	PtType *string `json:"pttype"`
	// UUID and PartUUID are captured for one reason: a "copy UUID" button beside each partition
	// (`FEATURES.md` A28). They are what `/etc/fstab` and `blkid` speak in, so somebody adding this
	// drive to a config needs one — but as columns they are two long opaque strings per row with
	// nothing to scan, which is why they are a button and not a field.
	UUID     *string `json:"uuid"`
	PartUUID *string `json:"partuuid"`
}

// Partition is one row of a drive's partition schema (`FEATURES.md` A28).
//
// Flat, in tree order, with the node name carrying the nesting: `/dev/sda1` is a partition,
// `/dev/mapper/vg-data` is a logical volume inside one. A depth field would say the same thing twice
// and give the UI a second thing to get wrong.
type Partition struct {
	Name string `json:"name"`
	// Type is lsblk's: `part`, `lvm`, `crypt`, `rom`. Kept as reported rather than mapped, because
	// the set is open and the value is the answer to "what kind of thing is this".
	Type         string   `json:"type"`
	SizeBytes    uint64   `json:"size_bytes"`
	Fstype       string   `json:"fstype,omitempty"`
	FsVersion    string   `json:"fs_version,omitempty"`
	Label        string   `json:"label,omitempty"`
	PartLabel    string   `json:"part_label,omitempty"`
	PartTypeName string   `json:"part_type,omitempty"`
	Mountpoints  []string `json:"mountpoints,omitempty"`
	// Usage, absent when the container cannot see the mountpoint. Absent is the honest answer: a
	// zero here would read as an empty filesystem.
	UsedBytes  *uint64 `json:"used_bytes,omitempty"`
	SizeOnDisk *uint64 `json:"fs_size_bytes,omitempty"`
	AvailBytes *uint64 `json:"avail_bytes,omitempty"`
	UsePercent string  `json:"use_percent,omitempty"`
	ReadOnly   bool    `json:"read_only,omitempty"`
	// State is lsblk's, e.g. `running`. Empty for a partition, which does not have one.
	State string `json:"state,omitempty"`
	// UUID is the filesystem's, PartUUID the partition table's. Carried for the copy button rather
	// than for display: they are what `/etc/fstab` speaks in, and nobody reads them.
	UUID     string `json:"uuid,omitempty"`
	PartUUID string `json:"part_uuid,omitempty"`
}

// Fullest is the filesystem with the least room left, which is the one number that answers "is this
// drive full?" — and it is not the same as the average. A drive holding a 20%-full boot partition and
// a 95%-full data volume is a drive that is about to run out of space, and a blended figure would hide
// exactly the filesystem somebody needs to be told about.
//
// Returns false when nothing on the drive has a readable usage figure — no mountpoints, or none the
// container can see — which is a different answer from "0% used".
func (l DiskLayout) Fullest() (Partition, bool) {
	best := Partition{}
	found := false
	for _, p := range l.Partitions {
		pct, ok := p.UsePercentValue()
		if !ok {
			continue
		}
		if !found || pct > mustPercent(best) {
			best = p
			found = true
		}
	}
	return best, found
}

// UsePercentValue parses lsblk's formatted percentage ("47%") into a number.
//
// lsblk formats this, so it arrives as a string; parsing it here rather than in the browser keeps one
// implementation of "which filesystem is fullest" for the list and the detail page both.
func (p Partition) UsePercentValue() (int, bool) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(p.UsePercent), "%")
	if trimmed == "" {
		return 0, false
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func mustPercent(p Partition) int {
	n, _ := p.UsePercentValue()
	return n
}

// DiskLayout is a drive's partition schema and the storage facts that belong with it
// (`FEATURES.md` A28), in the shape that is stored and served.
//
// One struct rather than five columns, for the same reason the snapshot attributes blob is one: it is
// read whole, never queried by, and every field comes from the same single `lsblk` call. Splitting it
// would mean five more columns on `devices` and five more places to forget one.
type DiskLayout struct {
	Partitions []Partition `json:"partitions"`
	// UnallocatedBytes is approximate — see the field on Discovered. Absent when there is no
	// partition table to reason about.
	UnallocatedBytes *uint64 `json:"unallocated_bytes,omitempty"`
	// Table is lsblk's PTTYPE (gpt, dos), absent when there is no table.
	Table string `json:"table,omitempty"`
	// Sector sizes and discard support: the facts that live behind a disclosure rather than in the
	// table. Absent when the device did not report them.
	LogSectorSize  int  `json:"log_sector_size,omitempty"`
	PhySectorSize  int  `json:"phy_sector_size,omitempty"`
	DiscardSupport bool `json:"discard_support,omitempty"`
}

// DiskLayout gathers what `lsblk` reported for this drive, or nil when it reported nothing worth
// storing.
//
// Nil rather than an empty struct: a drive whose layout was never read and a drive with no
// partitions are different findings, and the second one is worth saying out loud.
func (d Discovered) DiskLayout() *DiskLayout {
	if len(d.Partitions) == 0 && d.PartitionTable == "" &&
		d.LogSectorSize == 0 && d.PhySectorSize == 0 && d.UnallocatedBytes == nil {
		return nil
	}
	return &DiskLayout{
		Partitions:       d.Partitions,
		UnallocatedBytes: d.UnallocatedBytes,
		Table:            d.PartitionTable,
		LogSectorSize:    d.LogSectorSize,
		PhySectorSize:    d.PhySectorSize,
		DiscardSupport:   d.DiscardSupport,
	}
}

// DiscardSupport reports whether the device advertises discard (TRIM) at all.
//
// A granularity of zero means it does not — which for an SSD behind a USB bridge is a real finding:
// without discard the drive cannot know which blocks are free, so its write performance degrades as
// it fills. It is a fact about the bridge as much as the drive, and this app's audience is people
// with drives in enclosures.
func (d lsblkDev) DiscardSupport() bool {
	return d.DiscGran != nil && *d.DiscGran > 0
}

type smartScanOut struct {
	Devices []struct {
		Name     string `json:"name"`
		InfoName string `json:"info_name"`
		Type     string `json:"type"`
	} `json:"devices"`
}

// ListDevices discovers block devices. lsblk is the source of truth for coverage
// ("if it shows up in lsblk it shows up in the UI"); smartctl --scan-open
// contributes the transport hint needed for some USB bridges.
func ListDevices(ctx context.Context, timeout time.Duration) ([]Discovered, error) {
	lsblkRes, err := Run(ctx, timeout, "lsblk",
		"--json", "--paths", "--bytes",
		"--output", "NAME,KNAME,MODEL,SERIAL,SIZE,TYPE,TRAN,MOUNTPOINTS,FSTYPE,FSVER,REV,ROTA,RM,WWN,"+
			// The partition schema (`FEATURES.md` A28). Every one of these was already being
			// fetched and then discarded — the columns cost one argument, and the tree below is
			// walked anyway to collect the disk's mountpoints.
			"LABEL,PARTLABEL,PARTTYPENAME,PARTN,STATE,RO,FSUSE%,FSUSED,FSSIZE,FSAVAIL,"+
			"LOG-SEC,PHY-SEC,DISC-GRAN,DISC-MAX,DISC-ZERO,PTTYPE,UUID,PARTUUID",
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

		// A28: and keep the tree, which is what the flattening above used to throw away.
		partitions := collectPartitions(d)

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

			Partitions:       partitions,
			UnallocatedBytes: unallocated(d, partitions),
			PartitionTable:   deref(d.PtType),
			LogSectorSize:    intOr(d.LogSec, 0),
			PhySectorSize:    intOr(d.PhySec, 0),
			DiscardSupport:   d.DiscardSupport(),
		})
	}
	return out, nil
}

// collectPartitions walks a disk's subtree and returns one row per node below it, in tree order.
//
// Every node, not only the ones lsblk calls `part`: a logical volume or a dm-crypt mapping inside a
// partition is a thing with a size, a filesystem and a mountpoint, and leaving it out would describe
// a drive as having an unexplained partition rather than a volume group. The node name carries the
// nesting (`/dev/mapper/vg-data`), so no depth field is needed.
//
// A disk with no children returns nothing rather than a row for itself. The disk is already the
// subject of the page; a partition table with one entry that says "the whole disk" would be a row
// that tells the reader nothing they did not already know.
func collectPartitions(disk lsblkDev) []Partition {
	var out []Partition
	var walk func(d lsblkDev)
	walk = func(d lsblkDev) {
		for _, child := range d.Children {
			out = append(out, Partition{
				Name:         child.Name,
				Type:         child.Type,
				SizeBytes:    child.Size,
				Fstype:       deref(child.Fstype),
				FsVersion:    deref(child.Fsver),
				Label:        deref(child.Label),
				PartLabel:    deref(child.PartLabel),
				PartTypeName: deref(child.PartTypeName),
				Mountpoints:  cleanMountpoints(child.Mountpoints),
				UsedBytes:    child.FsUsed,
				SizeOnDisk:   child.FsSize,
				AvailBytes:   child.FsAvail,
				UsePercent:   deref(child.Fsuse),
				ReadOnly:     child.Ro,
				State:        deref(child.State),
				UUID:         deref(child.UUID),
				PartUUID:     deref(child.PartUUID),
			})
			walk(child)
		}
	}
	walk(disk)
	return out
}

// unallocated is how much of a disk no partition claims, or nil when the question does not apply.
//
// Approximate on purpose, and the caller is told so: alignment gaps and the backup GPT header are
// real bytes this does not count, and a partition whose size lsblk could not read contributes
// nothing. It answers "is this drive mostly empty?", where being a gigabyte out is irrelevant.
//
// Nil rather than zero for a disk with no partition table, and nil when the partitions add up to
// more than the disk — the latter happens with an extended partition, whose size overlaps its own
// logical partitions. Reporting "0 bytes unallocated" there would be a confident wrong answer.
func unallocated(disk lsblkDev, partitions []Partition) *uint64 {
	if deref(disk.PtType) == "" || len(partitions) == 0 {
		return nil
	}
	// Only the direct children, which is where a partition table's entries live. Summing nested
	// volumes too would double-count everything inside a partition.
	var claimed uint64
	for _, child := range disk.Children {
		claimed += child.Size
	}
	if claimed == 0 || claimed >= disk.Size {
		return nil
	}
	left := disk.Size - claimed
	return &left
}

// intOr dereferences a nullable int, or returns fallback.
func intOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
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
