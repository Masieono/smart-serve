package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// deviceJSON is the API representation of a device (docs/API.md).
type deviceJSON struct {
	Serial          string   `json:"serial"`
	Model           string   `json:"model"`
	ModelFamily     string   `json:"model_family,omitempty"`
	Firmware        string   `json:"firmware,omitempty"`
	DevicePath      string   `json:"device_path"`
	Transport       string   `json:"transport"`
	SmartBus        string   `json:"smart_bus,omitempty"`
	CapacityBytes   *uint64  `json:"capacity_bytes,omitempty"`
	WWN             string   `json:"wwn,omitempty"`
	Rotation        *int     `json:"rotation,omitempty"`
	Health          string   `json:"health"`
	HealthRationale string   `json:"health_rationale"`
	Score           int      `json:"score"`
	TemperatureC    *int     `json:"temperature_c,omitempty"`
	SmartAvailable  bool     `json:"smart_available"`
	SmartError      string   `json:"smart_error,omitempty"`
	Mountpoints     []string `json:"mountpoints"`
	Fstype          string   `json:"fstype,omitempty"`
	// Removable and FsVersion come from lsblk. For a drive that reports no S.M.A.R.T.
	// they are often the only description there is.
	Removable *bool  `json:"removable,omitempty"`
	FsVersion string `json:"fs_version,omitempty"`
	// SmartD is the remembered `smartctl -d` for this drive, so the UI can show which
	// override is active rather than only offering to set one.
	SmartD    string  `json:"smart_d,omitempty"`
	Name      string  `json:"name"`
	Notes     string  `json:"notes"`
	Tags      string  `json:"tags"`
	Hidden    bool    `json:"hidden"`
	FirstSeen string  `json:"first_seen"`
	LastSeen  string  `json:"last_seen"`
	LastScan  *string `json:"last_scan"`
	IsMissing bool    `json:"is_missing"`
}

func toDeviceJSON(d store.DeviceRecord) deviceJSON {
	out := deviceJSON{
		Serial:          d.Serial,
		Model:           d.Model,
		ModelFamily:     d.ModelFamily,
		Firmware:        d.Firmware,
		DevicePath:      d.DevicePath,
		Transport:       d.Transport,
		SmartBus:        d.SmartBus,
		CapacityBytes:   d.CapacityBytes,
		WWN:             d.WWN,
		Rotation:        d.Rotation,
		Removable:       d.Removable,
		FsVersion:       d.FsVersion,
		SmartD:          d.TransportOverride,
		Health:          d.Health,
		HealthRationale: d.HealthRationale,
		Score:           d.Score,
		TemperatureC:    d.TemperatureC,
		SmartAvailable:  d.SmartAvailable,
		SmartError:      d.SmartError,
		Mountpoints:     d.Mountpoints,
		Fstype:          d.Fstype,
		Name:            d.Name,
		Notes:           d.Notes,
		Tags:            d.Tags,
		Hidden:          d.Hidden,
		FirstSeen:       rfc3339(d.FirstSeen),
		LastSeen:        rfc3339(d.LastSeen),
		IsMissing:       d.IsMissing,
	}
	if d.LastScan != nil {
		s := rfc3339(*d.LastScan)
		out.LastScan = &s
	}
	if out.Mountpoints == nil {
		out.Mountpoints = []string{}
	}
	return out
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, "list devices", err)
		return
	}
	out := make([]deviceJSON, 0, len(devices))
	for _, d := range devices {
		out = append(out, toDeviceJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"devices":      out,
		"last_scan_ts": s.lastScanTime(r.Context()),
	})
}

func (s *Server) lastScanTime(ctx context.Context) any {
	history, err := s.store.ListScanHistory(ctx, 1)
	if err != nil || len(history) == 0 {
		return nil
	}
	return rfc3339(history[0].EndedAt)
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	rec, err := s.store.GetDevice(r.Context(), serial)
	if err != nil {
		s.fail(w, "get device", err)
		return
	}
	if rec == nil {
		writeError(w, http.StatusNotFound, "unknown device")
		return
	}

	resp := map[string]any{"device": toDeviceJSON(*rec)}
	if snap, err := s.store.LatestSnapshot(r.Context(), serial); err == nil && snap != nil {
		resp["snapshot"] = map[string]any{
			"ts":             rfc3339(snap.TS),
			"scan_id":        snap.ScanID,
			"health":         snap.Health,
			"score":          snap.Score,
			"temperature_c":  snap.TemperatureC,
			"power_on_hours": snap.PowerOnHours,
			"wear_pct":       snap.WearPct,
			"attributes":     rawOrNull(snap.Attributes),
		}
		resp["metrics"] = snapshotMetrics(snap)
	}
	writeJSON(w, http.StatusOK, resp)
}

// snapshotPayload is the decoded form of the attributes blob stored with each
// snapshot (docs/DATA_MODEL.md).
type snapshotPayload struct {
	Attributes []model.Attribute `json:"attributes"`
	NVMe       *model.NVMeHealth `json:"nvme"`
}

// decodeSnapshot parses a snapshot's attributes blob, tolerating anything malformed
// so a corrupt row can never break a report.
func decodeSnapshot(snap *store.Snapshot) snapshotPayload {
	var payload snapshotPayload
	if snap == nil || strings.TrimSpace(snap.Attributes) == "" {
		return payload
	}
	_ = json.Unmarshal([]byte(snap.Attributes), &payload)
	return payload
}

// snapshotMetrics derives throughput, power cycles and the like from the stored
// attributes blob, so the UI does not have to understand S.M.A.R.T. layouts.
func snapshotMetrics(snap *store.Snapshot) interp.Metrics {
	if snap == nil {
		return interp.Metrics{}
	}
	payload := decodeSnapshot(snap)
	return interp.DeriveMetrics(&model.SmartData{
		Attributes:   payload.Attributes,
		NVMe:         payload.NVMe,
		PowerOnHours: snap.PowerOnHours,
	})
}

func (s *Server) handleSetNotes(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	var body struct {
		Name   *string `json:"name"`
		Notes  *string `json:"notes"`
		Tags   *string `json:"tags"`
		Hidden *bool   `json:"hidden"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	// Full replace semantics: an omitted field clears the stored value.
	n := store.Notes{Serial: serial}
	if body.Name != nil {
		n.Name = *body.Name
	}
	if body.Notes != nil {
		n.Notes = *body.Notes
	}
	if body.Tags != nil {
		n.Tags = *body.Tags
	}
	if body.Hidden != nil {
		n.Hidden = *body.Hidden
	}
	if err := s.store.SetNotes(r.Context(), n); err != nil {
		s.fail(w, "set notes", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"serial": serial, "name": n.Name, "notes": n.Notes, "tags": n.Tags, "hidden": n.Hidden,
	})
}

func (s *Server) handleSetTransport(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	var body struct {
		SmartD string `json:"smart_d"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.store.SetTransportOverride(r.Context(), serial, body.SmartD); err != nil {
		s.fail(w, "set transport", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"serial": serial, "smart_d": body.SmartD})
}
