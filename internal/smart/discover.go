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
	// DiskFstype is the *disk node's own* FSTYPE, as opposed to `Fstype` above, which is every
	// filesystem found anywhere in the subtree.
	//
	// It is the answer for a drive with no partitions: `linux_raid_member` on a disk pulled from an
	// array, `zfs_member` on a pool member, `ext4` on a filesystem written straight to the device.
	// Without it the only thing to say about such a drive is "unpartitioned", which is true and
	// useless — the interesting fact is *what* is on it (`FEATURES.md` A28).
	DiskFstype string `json:"disk_fstype,omitempty"`
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
	// TopLevel is true for a direct child of the disk — the nodes laid out side by side on it. False
	// for anything nested inside another partition: a logical volume or a dm-crypt mapping. Only the
	// disk map reads it (`FEATURES.md` A28); the table draws every row either way.
	TopLevel bool `json:"top_level,omitempty"`
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
	// SizeBytes is the disk's own capacity, which is the disk map's denominator (`FEATURES.md` A28).
	// It is the *reported* size rather than the sum of the partitions, so free space can be drawn as
	// the difference — and so a disk whose partitions do not add up still draws at the right width.
	SizeBytes uint64 `json:"size_bytes,omitempty"`
	// WholeDiskFstype is what the disk node itself reports when it holds no partition table: a
	// filesystem written directly to the device, or an array member signature.
	//
	// This is the field that turns "this drive has no partition table" into something worth
	// reading. A disk pulled from a RAID array carries `linux_raid_member` and no table at all —
	// which is not a fault, it is what an array member looks like on its own, and the app can say so
	// instead of implying the drive is unset-up (`FEATURES.md` A28).
	WholeDiskFstype string `json:"whole_disk_fstype,omitempty"`
	// WholeDiskName is what the signature above is a member of — "a Linux md array", "a ZFS pool" —
	// or empty when it is a plain filesystem. The mapping lives in `smart` beside the token list,
	// because it is knowledge about the tokens rather than about the page.
	WholeDiskName string `json:"whole_disk_name,omitempty"`
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
		SizeBytes:        d.SizeBytes,
		UnallocatedBytes: d.UnallocatedBytes,
		Table:            d.PartitionTable,
		LogSectorSize:    d.LogSectorSize,
		PhySectorSize:    d.PhySectorSize,
		DiscardSupport:   d.DiscardSupport,
		// Only when there is no partition table: with partitions, the disk node's own signature is
		// the table, and reporting it as "what is on this disk" would be doubly wrong.
		WholeDiskFstype: wholeDiskFstype(d.PartitionTable, d.DiskFstype),
		WholeDiskName:   WholeDiskMemberName(wholeDiskFstype(d.PartitionTable, d.DiskFstype)),
	}
}

// wholeDiskFstype is the disk's own signature, kept only when it is the thing occupying the disk.
//
// A partitioned disk's `pttype` is what is on it, and lsblk reports the *partition table* as the
// disk node's fstype on some systems — which would read as "this disk holds a gpt filesystem".
func wholeDiskFstype(table, diskFstype string) string {
	if strings.TrimSpace(table) != "" {
		return ""
	}
	return strings.TrimSpace(diskFstype)
}

// memberSignatures maps a whole-disk signature to the thing the disk is a member of.
//
// These are the blkid tokens for "this device is not a filesystem, it is part of something bigger".
// The list is closed in practice — they are the names the kernel's superblock probes write — so
// recognising them by name is the whole mechanism; there is no vendor-neutral way to ask "is this
// RAID?".
//
// The values are noun phrases that read after "a member of", because that is the sentence they end
// up in: "a member of a Linux md array". The raw token is shown beside it, so the page carries both
// the readable answer and the exact thing `blkid` said.
var memberSignatures = map[string]string{
	// Arrays.
	"linux_raid_member":             "a Linux md array",
	"isw_raid_member":               "an Intel Rapid Storage array",
	"ddf_raid_member":               "a DDF array",
	"lsi_mega_raid_member":          "an LSI MegaRAID array",
	"adaptec_raid_member":           "an Adaptec array",
	"jmicron_raid_member":           "a JMicron array",
	"nvidia_raid_member":            "an NVIDIA array",
	"promise_fasttrack_raid_member": "a Promise FastTrack array",
	"silicon_medley_raid_member":    "a Silicon Image array",
	"via_raid_member":               "a VIA array",
	"highpoint_raid_member":         "a HighPoint array",
	// Pools. Not arrays, and not faults either: a disk doing its job, with nothing to mount on its
	// own. Grouped with the arrays because the *answer* is the same — "there is nothing to mount
	// here, and that is expected" — even though the mechanism differs.
	"zfs_member":  "a ZFS pool",
	"lvm2_member": "an LVM volume group",
	"bcache":      "a bcache cache or backing device",
	"crypto_luks": "an encrypted volume",
}

// WholeDiskMemberName returns what a whole-disk signature is a member of, or "" for anything else.
//
// Empty means the signature is a plain filesystem written to the device, which needs no translation:
// `ext4` is what it says it is.
func WholeDiskMemberName(signature string) string {
	return memberSignatures[strings.TrimSpace(strings.ToLower(signature))]
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
			DiskFstype:       deref(d.Fstype),
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
//
// `TopLevel` marks the direct children of the disk, and it exists for the disk map (`FEATURES.md`
// A28). The table can do without it — the node name carries the nesting, so `/dev/mapper/vg-data`
// is visibly inside something — but a map of the *disk* has to know which nodes are laid out side by
// side and which sit inside another. A logical volume drawn beside the partition containing it would
// claim the disk is bigger than it is. The earlier "no depth field is needed" was right about the
// table and wrong about the map.
func collectPartitions(disk lsblkDev) []Partition {
	var out []Partition
	var walk func(d lsblkDev, topLevel bool)
	walk = func(d lsblkDev, topLevel bool) {
		for _, child := range d.Children {
			out = append(out, Partition{
				TopLevel:     topLevel,
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
			walk(child, false)
		}
	}
	walk(disk, true)
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
