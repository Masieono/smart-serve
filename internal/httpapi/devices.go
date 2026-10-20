package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// deviceJSON is the API representation of a device (docs/API.md).
type deviceJSON struct {
	Serial          string  `json:"serial"`
	Model           string  `json:"model"`
	ModelFamily     string  `json:"model_family,omitempty"`
	Firmware        string  `json:"firmware,omitempty"`
	DevicePath      string  `json:"device_path"`
	Transport       string  `json:"transport"`
	SmartBus        string  `json:"smart_bus,omitempty"`
	CapacityBytes   *uint64 `json:"capacity_bytes,omitempty"`
	WWN             string  `json:"wwn,omitempty"`
	Rotation        *int    `json:"rotation,omitempty"`
	Health          string  `json:"health"`
	HealthRationale string  `json:"health_rationale"`
	Score           int     `json:"score"`
	TemperatureC    *int    `json:"temperature_c,omitempty"`
	SmartAvailable  bool    `json:"smart_available"`
	SmartError      string  `json:"smart_error,omitempty"`
	// SmartCause names *why* the read failed. Three unrelated problems — a blocked
	// device node, a refused query, and a drive with no S.M.A.R.T. — all arrive as the
	// same "Operation not permitted", so the UI cannot tell them apart from the text.
	SmartCause  string   `json:"smart_cause,omitempty"`
	Mountpoints []string `json:"mountpoints"`
	Fstype      string   `json:"fstype,omitempty"`
	// Worsening is how many bad sectors appeared over the recent window, present only
	// when the count is moving. A drive whose count is moving is worth replacing before
	// it stops moving at all, so this is the dashboard's early warning.
	Worsening *int64 `json:"worsening,omitempty"`
	// PowerOnHours and WearPct come from the newest snapshot rather than from `devices`, because
	// they are readings rather than current state. Present on the list so a fleet table can show
	// each drive's age per row, without opening every drive to find it.
	PowerOnHours *int64   `json:"power_on_hours,omitempty"`
	WearPct      *float64 `json:"wear_pct,omitempty"`
	// Removable and FsVersion come from lsblk. For a drive that reports no S.M.A.R.T.
	// they are often the only description there is.
	Removable *bool  `json:"removable,omitempty"`
	FsVersion string `json:"fs_version,omitempty"`
	// SmartD is the remembered `smartctl -d` for this drive, so the UI can show which
	// override is active rather than only offering to set one.
	SmartD string `json:"smart_d,omitempty"`
	Name   string `json:"name"`
	Notes  string `json:"notes"`
	Tags   string `json:"tags"`
	Hidden bool   `json:"hidden"`
	// Bay is where the drive physically is, so a card can answer "which one do I pull?" without
	// the reader matching serials against a spreadsheet.
	Bay       string  `json:"bay,omitempty"`
	FirstSeen string  `json:"first_seen"`
	LastSeen  string  `json:"last_seen"`
	LastScan  *string `json:"last_scan"`
	IsMissing bool    `json:"is_missing"`
	// Logs summarises what the drive's own error and self-test logs hold. Present only on
	// the detail endpoint, and only when the drive reported something: parsing the stored
	// raw document for every device on every list request would be work for data the
	// dashboard does not show.
	Logs *model.LogSummary `json:"logs,omitempty"`
	// Changes is what moved between the last two scans (`FEATURES.md` A18). Detail endpoint
	// only, for the same reason as Logs: it needs two snapshots, and the list has no place to
	// put the answer.
	Changes *changesJSON `json:"changes,omitempty"`
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
		SmartCause:      string(smart.ClassifyFailure(d.SmartError)),
		Mountpoints:     d.Mountpoints,
		Fstype:          d.Fstype,
		Name:            d.Name,
		Notes:           d.Notes,
		Tags:            d.Tags,
		Hidden:          d.Hidden,
		Bay:             d.Bay,
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

// logsFrom summarises the drive's own logs out of the raw document stored with it.
//
// Nil when the drive reported nothing, so the UI renders no panel rather than an empty one:
// "we did not look" and "there is nothing there" are different findings, and an empty panel
// reads as the first.
func logsFrom(raw string) *model.LogSummary {
	summary := smart.ParseLogs([]byte(raw))
	if summary.IsEmpty() {
		return nil
	}
	return &summary
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	rows, ranking, err := s.deviceRows(r.Context())
	if err != nil {
		s.fail(w, "list devices", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"devices": rows,
		// A24: "which one do I buy this month?" is a different question from the sort the
		// dashboard already does, because it is about direction and remaining life as much as
		// about the verdict. Built from the payloads just assembled, so the ranking and the cards
		// cannot disagree about a drive.
		"ranking":      ranking,
		"last_scan_ts": s.lastScanTime(r.Context()),
	})
}

// deviceRows builds the device list payload and its replacement ranking together.
//
// Extracted from handleListDevices so the fleet export (`FEATURES.md` A23) renders the same rows the
// dashboard shows, through the same ranking. A second copy of this would be a second answer to "what
// is this drive", and the export's whole job is to be the version you can trust and file.
//
// The two derived queries — bad-sector growth, and power-on hours with wear — are best-effort. A
// failure in either is not worth failing the list for: the extra figures are a nicety and the list
// is not.
func (s *Server) deviceRows(ctx context.Context) ([]deviceJSON, []interp.ReplacementRank, error) {
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Bad-sector growth per device, so a drive that is getting worse is flagged without the user
	// having to open each one.
	trend, err := s.store.SectorTrend(ctx, time.Now().Add(-s.store.RetentionWindow(ctx)), interp.WorsenWindow)
	if err != nil {
		trend = nil
	}
	// Power-on hours and wear, for the same reason: a fleet table wants a drive's age per row, and
	// one query for the list beats one per row.
	facts, ferr := s.store.LatestSnapshotFacts(ctx)
	if ferr != nil {
		facts = nil
	}

	out := make([]deviceJSON, 0, len(devices))
	for _, d := range devices {
		dj := toDeviceJSON(d)
		if delta, ok := trend[d.Serial]; ok && delta > 0 {
			dj.Worsening = &delta
		}
		if f, ok := facts[d.Serial]; ok {
			dj.PowerOnHours, dj.WearPct = f.PowerOnHours, f.WearPct
		}
		out = append(out, dj)
	}
	return out, interp.RankForReplacement(replacementCandidates(out)), nil
}

// replacementCandidates maps the list payloads onto the ranking's inputs.
//
// A disconnected drive is deliberately left out. It is a finding of its own — the dashboard flags
// it and has a toggle for it — and its stored verdict is the last one ever *seen* rather than a
// reading anyone just took. Ranking it would put a backup drive someone unplugged on purpose above
// a drive that is actually failing.
func replacementCandidates(devices []deviceJSON) []interp.ReplacementCandidate {
	out := make([]interp.ReplacementCandidate, 0, len(devices))
	for _, d := range devices {
		if d.IsMissing {
			continue
		}
		var worsening int64
		if d.Worsening != nil {
			worsening = *d.Worsening
		}
		out = append(out, interp.ReplacementCandidate{
			Serial:    d.Serial,
			Name:      d.Name,
			Health:    model.Verdict(d.Health),
			Rationale: d.HealthRationale,
			Score:     d.Score,
			Worsening: worsening,
			WearPct:   d.WearPct,
		})
	}
	return out
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

	device := toDeviceJSON(*rec)
	device.Logs = logsFrom(rec.RawOutput)
	// A18: what changed since the previous scan. Two snapshots, newest first, and the diff is
	// computed here rather than in the UI so the "is this a finding?" rule stays beside the
	// interpretation that already owns it.
	if recent, err := s.store.RecentSnapshots(r.Context(), serial, 2); err == nil {
		var prev, cur *store.Snapshot
		if len(recent) > 0 {
			cur = &recent[0]
		}
		if len(recent) > 1 {
			prev = &recent[1]
		}
		device.Changes = diffSnapshots(prev, cur)
	}

	resp := map[string]any{
		"device":     device,
		"projection": s.deviceProjection(r.Context(), serial),
	}
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
		resp["metrics"] = snapshotMetrics(snap, rec.ModelFamily)
	}
	writeJSON(w, http.StatusOK, resp)
}

// projectionWindow is how far back the longevity estimate looks, and it is the user's own
// `retention_days` setting (`A26`).
//
// It used to be a fixed 180 days, which meant an owner who raised retention to five years to watch
// a slow trend had that history discarded by the projection anyway. The window is now the same
// number the pruning uses, so what the user keeps is what the app reads.
//
// Kept as a helper rather than inlined so the three callers cannot drift apart — and so the
// reasoning has somewhere to live.
func (s *Server) projectionWindow(ctx context.Context) time.Duration {
	return s.store.RetentionWindow(ctx)
}

// deviceProjection builds the longevity estimate and the worsening flag from stored
// history.
//
// A failure here is not worth failing the page for: an absent projection renders as "not
// enough history yet", which is what a young install would show anyway.
func (s *Server) deviceProjection(ctx context.Context, serial string) interp.Projection {
	now := time.Now()
	series, err := s.store.SnapshotSeries(ctx, serial, now.Add(-s.projectionWindow(ctx)), now)
	if err != nil {
		return interp.Projection{}
	}
	obs := make([]interp.Observation, 0, len(series))
	for _, snap := range series {
		obs = append(obs, interp.Observation{
			TS:      snap.TS,
			WearPct: snap.WearPct,
			Hours:   snap.PowerOnHours,
			Sectors: snap.Sectors,
		})
	}
	return interp.Project(obs)
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
//
// modelFamily is passed in rather than read from the snapshot because it is stored on the
// device, not per snapshot. It is not decoration: whether an ATA drive's attribute 177
// means "life remaining" depends on who made the drive (SMART_INTERPRETATION.md §1.1), so
// a payload rebuilt without it silently loses the life figure — which is exactly what
// happened the first time, and only an end-to-end run against a real drive's attributes
// caught it.
func snapshotMetrics(snap *store.Snapshot, modelFamily string) interp.Metrics {
	if snap == nil {
		return interp.Metrics{}
	}
	payload := decodeSnapshot(snap)
	return interp.DeriveMetrics(&model.SmartData{
		ModelFamily:  modelFamily,
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
		Bay    *string `json:"bay"`
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
	if body.Bay != nil {
		n.Bay = *body.Bay
	}
	if err := s.store.SetNotes(r.Context(), n); err != nil {
		s.fail(w, "set notes", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"serial": serial, "name": n.Name, "notes": n.Notes, "tags": n.Tags,
		"hidden": n.Hidden, "bay": n.Bay,
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
