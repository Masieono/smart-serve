package scan

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/masieono/smart-serve/internal/events"
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
}

// Manager runs scans, serializes them (single-flight), persists the outcome, and
// publishes progress to the SSE hub.
type Manager struct {
	runner Runner
	store  *store.Store
	hub    *events.Hub

	mu      sync.Mutex
	running bool
	done    chan struct{}
	last    Result
}

// NewManager wires a scan runner to persistence and the event hub.
func NewManager(r Runner, st *store.Store, hub *events.Hub) *Manager {
	return &Manager{runner: r, store: st, hub: hub}
}

// Scan runs a scan, or joins the one already in flight. The joined return value
// reports whether the caller waited on an existing scan rather than starting one.
func (m *Manager) Scan(ctx context.Context, trigger string) (res Result, joined bool, err error) {
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

	res, err = m.run(ctx, trigger)

	m.mu.Lock()
	m.last = res
	m.running = false
	m.mu.Unlock()
	close(done)
	return res, false, err
}

// Last returns the most recent scan result (zero value before the first scan).
func (m *Manager) Last() Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

func (m *Manager) run(ctx context.Context, trigger string) (Result, error) {
	started := time.Now().UTC()
	m.hub.Publish("scan_started", map[string]any{"started_at": started})

	devices, err := m.runner.Run(ctx)
	ended := time.Now().UTC()
	res := Result{Started: started, Ended: ended, Devices: devices}

	if err != nil {
		res.Error = err.Error()
		if id, serr := m.store.SaveFailedScan(ctx, trigger, started, ended, err.Error()); serr == nil {
			res.ScanID = id
		}
		m.hub.Publish("scan_complete", map[string]any{
			"scan_id":     res.ScanID,
			"ok":          false,
			"error":       err.Error(),
			"duration_ms": ended.Sub(started).Milliseconds(),
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
	scanID, err := m.store.SaveScan(ctx, trigger, started, ended, stored)
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

	m.prune(ctx)
	return res, nil
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
	out := store.ScannedDevice{
		Serial:          serialFor(d),
		Model:           strings.TrimSpace(d.Discovered.Model),
		DevicePath:      d.Discovered.Path,
		Transport:       d.Discovered.Transport,
		WWN:             d.Discovered.WWN,
		Mountpoints:     d.Discovered.Mountpoints,
		Fstype:          d.Discovered.Fstype,
		Health:          string(d.Health.Verdict),
		HealthRationale: d.Health.Rationale,
		Score:           d.Health.Score,
		Error:           d.Error,
	}
	size := d.Discovered.SizeBytes
	if sd := d.Data; sd != nil {
		out.SmartBus = string(sd.Bus)
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
