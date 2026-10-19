package httpapi

import (
	"encoding/csv"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// handleRaw serves the raw smartctl output for one drive.
//
// It reads from the store, not from the scanner's in-memory result. That result is
// empty before the first scan of a process — so every drive 404'd after a restart, with
// a populated database sitting right there — and a single-drive rescan replaces it with
// a one-device result, so rescanning one drive broke the raw view for all the others.
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	rec, err := s.store.GetDevice(r.Context(), serial)
	if err != nil {
		s.fail(w, "raw output", err)
		return
	}
	if rec == nil {
		writeError(w, http.StatusNotFound, "unknown device")
		return
	}
	if strings.TrimSpace(rec.RawOutput) == "" {
		writeError(w, http.StatusNotFound,
			"no raw smartctl output stored for this device yet — rescan it")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(rec.RawOutput))
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	from := parseTimeQuery(r, "from", time.Now().AddDate(0, 0, -180))
	to := parseTimeQuery(r, "to", time.Now().Add(time.Hour))

	series, err := s.store.SnapshotSeries(r.Context(), serial, from, to)
	if err != nil {
		s.fail(w, "device history", err)
		return
	}
	out := make([]map[string]any, 0, len(series))
	for _, snap := range series {
		out = append(out, map[string]any{
			"ts":             rfc3339(snap.TS),
			"scan_id":        snap.ScanID,
			"health":         snap.Health,
			"score":          snap.Score,
			"temperature_c":  snap.TemperatureC,
			"power_on_hours": snap.PowerOnHours,
			"wear_pct":       snap.WearPct,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"serial": serial, "series": out})
}

func parseTimeQuery(r *http.Request, key string, def time.Time) time.Time {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return def
	}
	return t
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	rec, err := s.store.GetDevice(r.Context(), serial)
	if err != nil {
		s.fail(w, "device report", err)
		return
	}
	if rec == nil {
		writeError(w, http.StatusNotFound, "unknown device")
		return
	}
	snap, _ := s.store.LatestSnapshot(r.Context(), serial)

	switch format := r.URL.Query().Get("format"); format {
	case "", "txt":
		download(w, "text/plain; charset=utf-8", serial, "txt")
		_, _ = fmt.Fprint(w, renderTextReport(*rec, snap))
	case "json":
		body := map[string]any{"device": toDeviceJSON(*rec)}
		if snap != nil {
			body["snapshot"] = map[string]any{
				"ts":         rfc3339(snap.TS),
				"scan_id":    snap.ScanID,
				"attributes": rawOrNull(snap.Attributes),
			}
			body["metrics"] = snapshotMetrics(snap, rec.ModelFamily)
		}
		writeJSON(w, http.StatusOK, body)
	case "html":
		download(w, "text/html; charset=utf-8", serial, "html")
		_, _ = fmt.Fprint(w, renderHTMLReport(*rec, snap))
	case "csv":
		download(w, "text/csv; charset=utf-8", serial, "csv")
		_, _ = fmt.Fprint(w, renderCSVReport(*rec, snap))
	default:
		writeError(w, http.StatusBadRequest, "unsupported format (want txt, json, html or csv)")
	}
}

// download sets the attachment headers for an exported report.
func download(w http.ResponseWriter, contentType, serial, ext string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", "smart-serve-"+safeFilename(serial)+"."+ext))
}

func safeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, s)
}

// renderTextReport produces a CrystalDiskInfo-style plain-text report suitable
// for a warranty claim (docs/FEATURES.md A8).
func renderTextReport(d store.DeviceRecord, snap *store.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "smart-serve report\n==================\n\n")
	fmt.Fprintf(&b, "Device:       %s\n", firstNonEmpty(d.Name, d.Model))
	fmt.Fprintf(&b, "Model:        %s\n", d.Model)
	if d.ModelFamily != "" {
		fmt.Fprintf(&b, "Family:       %s\n", d.ModelFamily)
	}
	fmt.Fprintf(&b, "Serial:       %s\n", d.Serial)
	if d.Bay != "" {
		fmt.Fprintf(&b, "Bay:          %s\n", d.Bay)
	}
	fmt.Fprintf(&b, "Path:         %s\n", d.DevicePath)
	fmt.Fprintf(&b, "Transport:    %s\n", d.Transport)
	if d.Fstype != "" {
		fmt.Fprintf(&b, "Filesystem:   %s\n", d.Fstype)
	}
	if len(d.Mountpoints) > 0 {
		fmt.Fprintf(&b, "Mountpoints:  %s\n", strings.Join(d.Mountpoints, ", "))
	}
	fmt.Fprintf(&b, "Capacity:     %s\n", humanBytes(d.CapacityBytes))
	if d.WWN != "" {
		fmt.Fprintf(&b, "WWN:          %s\n", d.WWN)
	}
	fmt.Fprintf(&b, "\nHealth:       %s\n", strings.ToUpper(d.Health))
	fmt.Fprintf(&b, "Score:        %s\n", scoreText(d.Score))
	fmt.Fprintf(&b, "Assessment:   %s\n", d.HealthRationale)
	if d.TemperatureC != nil {
		fmt.Fprintf(&b, "Temperature:  %d C\n", *d.TemperatureC)
	}
	fmt.Fprintf(&b, "SMART:        %s\n", smartState(d))
	if d.LastScan != nil {
		fmt.Fprintf(&b, "Last scan:    %s\n", rfc3339(*d.LastScan))
	}
	if snap != nil && snap.PowerOnHours != nil {
		fmt.Fprintf(&b, "Power on:     %d hours\n", *snap.PowerOnHours)
	}
	if snap != nil && snap.WearPct != nil {
		fmt.Fprintf(&b, "Wear:         %.0f%%\n", *snap.WearPct)
	}
	fmt.Fprintf(&b, "\nGenerated by smart-serve\n")
	return b.String()
}

// reportField is one label/value pair in an exported report.
type reportField struct {
	label string
	value string
}

// reportFacts is the identity and health block shared by the HTML and CSV exports,
// so the two can never drift apart.
func reportFacts(d store.DeviceRecord, snap *store.Snapshot) []reportField {
	facts := []reportField{
		{"Device", firstNonEmpty(d.Name, d.Model)},
		{"Model", d.Model},
		{"Model family", d.ModelFamily},
		{"Serial", d.Serial},
		{"Firmware", d.Firmware},
		{"Bay", d.Bay},
		{"Path", d.DevicePath},
		{"Transport", d.Transport},
		{"Capacity", humanBytes(d.CapacityBytes)},
		{"WWN", d.WWN},
		{"Mountpoints", strings.Join(d.Mountpoints, ", ")},
		{"Filesystem", d.Fstype},
		{"Health", strings.ToUpper(d.Health)},
		{"Score", scoreText(d.Score)},
		{"Assessment", d.HealthRationale},
		{"S.M.A.R.T.", smartState(d)},
	}
	if snap != nil {
		if snap.TemperatureC != nil {
			facts = append(facts, reportField{"Temperature", fmt.Sprintf("%d C", *snap.TemperatureC)})
		}
		if snap.PowerOnHours != nil {
			facts = append(facts, reportField{"Power on", fmt.Sprintf("%d hours", *snap.PowerOnHours)})
		}
		if snap.WearPct != nil {
			facts = append(facts, reportField{"Wear", fmt.Sprintf("%.0f%%", *snap.WearPct)})
		}
	}
	if d.LastScan != nil {
		facts = append(facts, reportField{"Last scan", rfc3339(*d.LastScan)})
	}
	return facts
}

// nvmeFacts lists the NVMe health log for an export.
func nvmeFacts(n *model.NVMeHealth) []reportField {
	return []reportField{
		{"Critical warning", strconv.Itoa(n.CriticalWarning)},
		{"Available spare", fmt.Sprintf("%d%%", n.AvailableSpare)},
		{"Spare threshold", fmt.Sprintf("%d%%", n.AvailableSpareThresh)},
		{"Percentage used", fmt.Sprintf("%d%%", n.PercentageUsed)},
		{"Data units read", strconv.FormatInt(n.DataUnitsRead, 10)},
		{"Data units written", strconv.FormatInt(n.DataUnitsWritten, 10)},
		{"Host read commands", strconv.FormatInt(n.HostReads, 10)},
		{"Host write commands", strconv.FormatInt(n.HostWrites, 10)},
		{"Controller busy time", fmt.Sprintf("%d min", n.ControllerBusyTime)},
		{"Power cycles", strconv.FormatInt(n.PowerCycles, 10)},
		{"Power-on hours", strconv.FormatInt(n.PowerOnHours, 10)},
		{"Unsafe shutdowns", strconv.FormatInt(n.UnsafeShutdowns, 10)},
		{"Media errors", strconv.FormatInt(n.MediaErrors, 10)},
		{"Error log entries", strconv.FormatInt(n.ErrorLogEntries, 10)},
		{"Warning temp time", fmt.Sprintf("%d min", n.WarningTempTime)},
		{"Critical comp time", fmt.Sprintf("%d min", n.CriticalCompTime)},
	}
}

// reportCSS keeps an exported page self-contained: it has to stay readable years
// from now, as an email attachment, with no network and no app running.
const reportCSS = "body{font:14px/1.5 system-ui,-apple-system,sans-serif;margin:2rem;color:#1f2328;background:#fff}" +
	"h1{font-size:1.4rem;margin:0 0 .25rem}h2{font-size:1rem;margin:1.75rem 0 .5rem}" +
	".sub{color:#59636e;margin-bottom:1.5rem}" +
	"dl{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:.75rem 1.5rem;margin:0}" +
	"dt{color:#59636e;font-size:.75rem;text-transform:uppercase;letter-spacing:.03em}" +
	"dd{margin:.1rem 0 0;font-weight:600}" +
	"table{border-collapse:collapse;width:100%;font-size:.85rem}" +
	"th,td{border:1px solid #d8dee4;padding:.3rem .5rem;text-align:left}" +
	"th{background:#f0f3f6}td.num{text-align:right;font-variant-numeric:tabular-nums}" +
	"footer{margin-top:2rem;color:#59636e;font-size:.75rem}"

// renderHTMLReport produces a self-contained, printable page for sharing or for a
// support ticket (docs/FEATURES.md A8).
func renderHTMLReport(d store.DeviceRecord, snap *store.Snapshot) string {
	payload := decodeSnapshot(snap)

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>smart-serve report - " + html.EscapeString(d.Serial) + "</title>\n")
	b.WriteString("<style>" + reportCSS + "</style></head><body>\n")

	b.WriteString("<h1>" + html.EscapeString(firstNonEmpty(d.Name, d.Model)) + "</h1>\n")
	b.WriteString("<div class=\"sub\">" + html.EscapeString(d.Model) + " | " +
		html.EscapeString(d.Serial) + "</div>\n")

	b.WriteString("<h2>Summary</h2>\n<dl>\n")
	for _, f := range reportFacts(d, snap) {
		if strings.TrimSpace(f.value) == "" {
			continue
		}
		fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>\n", html.EscapeString(f.label), html.EscapeString(f.value))
	}
	b.WriteString("</dl>\n")

	if len(payload.Attributes) > 0 {
		b.WriteString("<h2>S.M.A.R.T. attributes</h2>\n<table>\n<thead><tr>")
		for _, h := range []string{"ID", "Attribute", "Value", "Worst", "Thresh", "Raw"} {
			b.WriteString("<th>" + h + "</th>")
		}
		b.WriteString("</tr></thead>\n<tbody>\n")
		for _, a := range payload.Attributes {
			fmt.Fprintf(&b, "<tr><td class=\"num\">%d</td><td>%s</td>"+
				"<td class=\"num\">%d</td><td class=\"num\">%d</td><td class=\"num\">%d</td>"+
				"<td class=\"num\">%s</td></tr>\n",
				a.ID, html.EscapeString(a.Name), a.Value, a.Worst, a.Threshold, html.EscapeString(rawText(a)))
		}
		b.WriteString("</tbody></table>\n")
	}

	if n := payload.NVMe; n != nil {
		b.WriteString("<h2>NVMe health log</h2>\n<dl>\n")
		for _, f := range nvmeFacts(n) {
			fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>\n", html.EscapeString(f.label), html.EscapeString(f.value))
		}
		b.WriteString("</dl>\n")
	}

	b.WriteString("<footer>Generated by smart-serve. S.M.A.R.T. data from smartmontools.</footer>\n")
	b.WriteString("</body></html>\n")
	return b.String()
}

// rawText prefers smartctl's own rendering of an attribute's raw value, falling back
// to the numeric form when it did not supply one.
func rawText(a model.Attribute) string {
	if a.RawString != "" {
		return a.RawString
	}
	return strconv.FormatUint(a.RawValue, 10)
}

// renderCSVReport emits a field/value sheet. A two-column shape stays readable for
// both families — an ATA drive has an attribute table, an NVMe drive a health log —
// and drops straight into a spreadsheet.
func renderCSVReport(d store.DeviceRecord, snap *store.Snapshot) string {
	payload := decodeSnapshot(snap)

	var b strings.Builder
	w := csv.NewWriter(&b)
	write := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		_ = w.Write([]string{field, value})
	}

	_ = w.Write([]string{"field", "value"})
	for _, f := range reportFacts(d, snap) {
		write(f.label, f.value)
	}
	for _, a := range payload.Attributes {
		write(fmt.Sprintf("attr %d %s", a.ID, a.Name), rawText(a))
	}
	if n := payload.NVMe; n != nil {
		for _, f := range nvmeFacts(n) {
			write("nvme "+f.label, f.value)
		}
	}
	w.Flush()
	return b.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "(unnamed)"
}

func scoreText(score int) string {
	if score < 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d/100", score)
}

func smartState(d store.DeviceRecord) string {
	if d.SmartAvailable {
		return "available"
	}
	if d.SmartError != "" {
		return "unavailable (" + d.SmartError + ")"
	}
	return "unavailable"
}

func humanBytes(p *uint64) string {
	if p == nil {
		return "unknown"
	}
	const unit = 1000
	units := []string{"B", "kB", "MB", "GB", "TB", "PB"}
	v := float64(*p)
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
