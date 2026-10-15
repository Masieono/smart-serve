package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// Result is the outcome of one scan.
type Result struct {
	ScanID  int64     `json:"scan_id"`
	Started time.Time `json:"started_at"`
	Ended   time.Time `json:"ended_at"`
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	Devices []Device  `json:"devices"`
}

// Runner produces the raw device results for one scan. *Scanner is the
// production implementation; tests substitute a fake.
type Runner interface {
	Run(ctx context.Context) ([]Device, error)
	// RunDevice rescans a single drive by serial. `path` is the drive's last-seen
	// device node, used as a fallback for bridges that report a different serial to
	// lsblk than the drive does to smartctl.
	RunDevice(ctx context.Context, serial, path string) (Device, error)
}

// Trigger describes what caused a scan. It is recorded in scan_history, and the
// kind decides whether a failure is notification-worthy (a manual scan's failure
// is already visible to whoever pressed the button).
type Trigger struct {
	Kind       string // manual | device | schedule | startup
	ScheduleID int64  // set when Kind is "schedule"
}

// Manual is the trigger for an operator-pressed full scan.
var Manual = Trigger{Kind: "manual"}

// DeviceScan is the trigger for an operator-pressed scan of one drive. (Named to
// avoid colliding with the Device type.)
var DeviceScan = Trigger{Kind: "device"}

// PostScan is the hand-off from the scan pipeline to anything that reacts to a
// finished scan. It exists so the scan package can invoke a notifier without
// importing it (see cmd/smartserve for the wiring).
type PostScan struct {
	Trigger Trigger
	ScanID  int64
	Started time.Time
	Ended   time.Time
	OK      bool
	Error   string

	// Previous is the stored device state from *before* this scan and Current
	// the state *after* it, both keyed by serial. A device absent from the scan
	// appears in Current with Missing set rather than being deleted, which is
	// what makes disappear/reappear detection possible.
	Previous map[string]store.DeviceState
	Current  map[string]store.DeviceState
}

// PostScanFunc reacts to a finished scan. It runs after the scan is committed,
// so it cannot affect the result, and it must not block for long — the next
// scheduled scan is waiting on the manager.
type PostScanFunc func(ctx context.Context, rep PostScan)

// Manager runs scans, serializes them (single-flight), persists the outcome, and
// publishes progress to the SSE hub.
type Manager struct {
	runner Runner
	store  *store.Store
	hub    *events.Hub

	// postScan, when set, is invoked after every scan (successful or not).
	postScan PostScanFunc

	mu      sync.Mutex
	running bool
	done    chan struct{}
	last    Result
}

// NewManager wires a scan runner to persistence and the event hub.
func NewManager(r Runner, st *store.Store, hub *events.Hub) *Manager {
	return &Manager{runner: r, store: st, hub: hub}
}

// OnPostScan registers the hook invoked after each scan. It is called outside the
// single-flight lock, so a slow hook delays only its own scan's completion.
func (m *Manager) OnPostScan(fn PostScanFunc) { m.postScan = fn }

// Scan runs a full scan, or joins the one already in flight. The joined return value
// reports whether the caller waited on an existing scan rather than starting one.
func (m *Manager) Scan(ctx context.Context, t Trigger) (res Result, joined bool, err error) {
	return m.start(ctx, t, only{})
}

// ScanDevice rescans one drive by serial. `path` is its last-seen device node, used
// to find a drive whose bridge reports a different serial than the drive does.
//
// It shares the single-flight lock with a full scan: the drive is about to be read
// from disk, and a concurrent scan would read it too.
func (m *Manager) ScanDevice(ctx context.Context, serial, path string, t Trigger) (res Result, joined bool, err error) {
	return m.start(ctx, t, only{serial: serial, path: path})
}

// only identifies a single-drive scan; the zero value means a full scan.
type only struct {
	serial string
	path   string
}

func (o only) full() bool { return o.serial == "" }

func (m *Manager) start(ctx context.Context, t Trigger, o only) (res Result, joined bool, err error) {
	m.mu.Lock()
	if m.running {
		done := m.done
		m.mu.Unlock()
		select {
		case <-done:
			m.mu.Lock()
			res = m.last
			m.mu.Unlock()
			return res, true, nil
		case <-ctx.Done():
			return Result{}, true, ctx.Err()
		}
	}
	m.running = true
	m.done = make(chan struct{})
	done := m.done
	m.mu.Unlock()

	res, err = m.run(ctx, t, o)

	m.mu.Lock()
	m.last = res
	m.running = false
	m.mu.Unlock()
	close(done)
	return res, false, err
}

// There is deliberately no accessor for the last result. The raw-output view used to
// read one, and depending on a value that only exists in the current process is what
// made it 404 after a restart. The result is still kept on the Manager for the
// joined-scan return above, but nothing outside should reach for it.

func (m *Manager) run(ctx context.Context, t Trigger, o only) (Result, error) {
	started := time.Now().UTC()
	m.hub.Publish("scan_started", map[string]any{"started_at": started, "serial": o.serial})

	// Capture the pre-scan state before the upsert overwrites it. That is what
	// lets the notifier see transitions rather than just current values.
	var previous map[string]store.DeviceState
	if m.postScan != nil {
		if prev, perr := m.store.DeviceStates(ctx); perr == nil {
			previous = prev
		}
	}

	var devices []Device
	var err error
	if o.full() {
		devices, err = m.runner.Run(ctx)
	} else if one, derr := m.runner.RunDevice(ctx, o.serial, o.path); derr != nil {
		err = derr
	} else {
		devices = []Device{one}
	}
	ended := time.Now().UTC()
	res := Result{Started: started, Ended: ended, Devices: devices}

	if err != nil {
		res.Error = err.Error()
		if id, serr := m.store.SaveFailedScan(ctx, t.Kind, started, ended, err.Error()); serr == nil {
			res.ScanID = id
		}
		m.hub.Publish("scan_complete", map[string]any{
			"scan_id":     res.ScanID,
			"ok":          false,
			"error":       err.Error(),
			"duration_ms": ended.Sub(started).Milliseconds(),
		})
		m.notify(ctx, PostScan{
			Trigger: t, ScanID: res.ScanID, Started: started, Ended: ended,
			OK: false, Error: err.Error(), Previous: previous,
		})
		return res, err
	}

	for _, d := range devices {
		m.hub.Publish("scan_device", map[string]any{
			"device": deviceName(d),
			"phase":  "done",
			"ok":     d.Error == "",
			"health": string(d.Health.Verdict),
		})
	}

	stored := make([]store.ScannedDevice, 0, len(devices))
	for _, d := range devices {
		stored = append(stored, toStored(d))
	}
	var scanID int64
	if o.full() {
		scanID, err = m.store.SaveScan(ctx, t.Kind, started, ended, stored)
	} else {
		// A single-drive scan must not mark the drives it did not look at as
		// disconnected: they were never checked.
		scanID, err = m.store.SavePartialScan(ctx, t.Kind, started, ended, stored)
	}
	if err != nil {
		res.Error = err.Error()
		return res, err
	}

	res.ScanID = scanID
	res.OK = true
	okCount := 0
	for _, d := range devices {
		if d.Error == "" {
			okCount++
		}
	}
	m.hub.Publish("scan_complete", map[string]any{
		"scan_id":        scanID,
		"ok":             true,
		"devices_total":  len(devices),
		"devices_ok":     okCount,
		"devices_failed": len(devices) - okCount,
		"duration_ms":    ended.Sub(started).Milliseconds(),
	})

	rep := PostScan{
		Trigger: t, ScanID: scanID, Started: started, Ended: ended,
		OK: true, Previous: previous,
	}
	if m.postScan != nil {
		if current, cerr := m.store.DeviceStates(ctx); cerr == nil {
			rep.Current = current
		}
	}
	m.notify(ctx, rep)

	m.prune(ctx)
	return res, nil
}

// notify invokes the post-scan hook, if any. A panic inside the hook is contained
// here: a notification bug must never take down the scan pipeline.
func (m *Manager) notify(ctx context.Context, rep PostScan) {
	if m.postScan == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			m.hub.Publish("notify_error", map[string]any{"error": fmt.Sprint(r)})
		}
	}()
	m.postScan(ctx, rep)
}

// prune applies the retention windows from settings. A pruning failure must
// never fail the scan itself.
func (m *Manager) prune(ctx context.Context) {
	retention, logRetention := 180, 30
	if settings, err := m.store.GetSettings(ctx); err == nil {
		retention = settingInt(settings, "retention_days", retention)
		logRetention = settingInt(settings, "notification_log_retention_days", logRetention)
	}
	_, _, _ = m.store.Prune(ctx, retention, logRetention)
}

func settingInt(settings map[string]string, key string, def int) int {
	v, ok := settings[key]
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// toStored maps a scan result onto the persistence shape.
func toStored(d Device) store.ScannedDevice {
	removable := d.Discovered.Removable
	out := store.ScannedDevice{
		Serial:      serialFor(d),
		Model:       strings.TrimSpace(d.Discovered.Model),
		DevicePath:  d.Discovered.Path,
		Transport:   d.Discovered.Transport,
		WWN:         d.Discovered.WWN,
		Mountpoints: d.Discovered.Mountpoints,
		Fstype:      d.Discovered.Fstype,
		Removable:   &removable,
		FsVersion:   d.Discovered.FsVersion,
		// lsblk's REV is the only firmware revision available when smartctl cannot be
		// read, which is exactly the case for a drive with no S.M.A.R.T.
		Firmware:        d.Discovered.Rev,
		Health:          string(d.Health.Verdict),
		HealthRationale: d.Health.Rationale,
		Score:           d.Health.Score,
		Error:           d.Error,
		// Kept verbatim even when the read failed: smartctl's own complaint is the most
		// useful thing to have in front of you when a bridge is misbehaving.
		RawOutput: string(d.Raw),
	}
	size := d.Discovered.SizeBytes
	if sd := d.Data; sd != nil {
		out.SmartBus = string(sd.Bus)
		out.ModelFamily = sd.ModelFamily
		if sd.Firmware != "" {
			out.Firmware = sd.Firmware
		}
		out.InterfaceSpeed = sd.InterfaceSpeed
		if sd.Model != "" {
			out.Model = sd.Model
		}
		if sd.WWN != "" {
			out.WWN = sd.WWN
		}
		if sd.CapacityBytes > 0 {
			size = sd.CapacityBytes
		}
		out.TemperatureC = sd.TemperatureC
		out.PowerOnHours = sd.PowerOnHours
		if sd.RotationRate > 0 {
			r := sd.RotationRate
			out.Rotation = &r
		}
		if sd.NVMe != nil {
			wear := float64(sd.NVMe.PercentageUsed)
			out.WearPct = &wear
		} else if left, ok := interp.ATALifeLeftPct(sd); ok {
			// Stored as percentage *used*, the same unit as NVMe's percentage_used, so one
			// longevity projection covers both transports.
			//
			// Before this, an ATA SSD had no wear figure at all — not because it lacked
			// one, but because only attribute 231 was read and most drives report the wear
			// somewhere else (177 on Samsung, 233 or 202 on others).
			wear := float64(100 - left)
			out.WearPct = &wear
		}
		// ATA wear counters are vendor-specific enough that guessing at one would be
		// worse than reporting none, so an ATA drive falls back to the sector trend.
		// Nil rather than zero for a drive that reports no attributes at all: "no sector
		// problems" and "does not report sectors" are different claims, and only the
		// first one is a clean bill of health.
		if len(sd.Attributes) > 0 {
			sectors := interp.SectorIssues(sd.Attributes)
			out.Sectors = &sectors
		}
		out.AttributesJSON = marshalAttributes(sd)
	}
	if size > 0 {
		out.CapacityBytes = &size
	}
	return out
}

// Running reports whether a scan is currently in flight, so callers can tell a
// fresh scan from one they will join.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Serial returns the stable identity for the device (ADR-005): the S.M.A.R.T.
// serial number, falling back to the lsblk serial and then to a model key.
func (d Device) Serial() string { return serialFor(d) }

// serialFor implements the identity rule from ADR-005.
func serialFor(d Device) string {
	if d.Data != nil {
		if s := strings.TrimSpace(d.Data.Serial); s != "" {
			return s
		}
	}
	if s := strings.TrimSpace(d.Discovered.Serial); s != "" {
		return s
	}
	name := strings.TrimSpace(d.Discovered.Model)
	if name == "" {
		name = d.Discovered.Path
	}
	return "__nomodel:" + name
}

func marshalAttributes(sd *model.SmartData) string {
	payload := struct {
		Attributes []model.Attribute `json:"attributes,omitempty"`
		NVMe       *model.NVMeHealth `json:"nvme,omitempty"`
	}{Attributes: sd.Attributes, NVMe: sd.NVMe}

	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}

func deviceName(d Device) string {
	if d.Data != nil && d.Data.Serial != "" {
		return d.Data.Serial
	}
	if d.Discovered.Serial != "" {
		return d.Discovered.Serial
	}
	return d.Discovered.Path
}
