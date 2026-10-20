package httpapi

import (
	"encoding/csv"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
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

// handleReport serves the report export for one drive (`FEATURES.md` A8), which doubles as the
// warranty/RMA evidence pack (`A27`).
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
		// The purpose is on this format too. "The exported report says what it is for" is easier to
		// explain as a rule than as three formats out of four, and a JSON file saved to a disk has
		// exactly the same problem as the others: it outlives the memory of where it came from.
		body := map[string]any{"device": toDeviceJSON(*rec), "purpose": reportPurpose}
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
	downloadAs(w, contentType, "smart-serve-"+safeFilename(serial)+"."+ext)
}

// downloadAs names the file being exported.
//
// The filename is part of the document: it is what the file is called in the folder it is saved to,
// months later, beside a dozen other exports. So it says what it is and when it was taken.
func downloadAs(w http.ResponseWriter, contentType, filename string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", filename))
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

// reportPurpose is what an exported report is for, and it is on every format (`FEATURES.md` A27).
//
// The report already existed (`A8`); saying what it is for costs one paragraph and is the whole
// reason some people run a scan at all. It is written for the person who *receives* the file — a
// support engineer, or the reader of a forum post — because a file found in a folder a year later
// has to explain itself without this repository to hand.
//
// It claims evidence, not proof. A S.M.A.R.T. report shows what a drive reported and what this app
// made of it; whether that is a warranty case is the vendor's call, and a tool that overstated its
// own authority would be the one thing that got a claim rejected.
const reportPurpose = "Warranty / RMA evidence. It records the drive's identity, the S.M.A.R.T. " +
	"data the drive reported, and this app's assessment of it. The data is read from the drive by " +
	"smartmontools and is not edited."

// reportNote is the closing remark on an export: where the underlying document is, and what this
// file is not.
const reportNote = "Generated by smart-serve. The raw smartctl document this was built from is " +
	"stored with the drive and can be exported from its page. S.M.A.R.T. data is the drive's own " +
	"report; it is evidence, not a diagnosis."

// failingAttributes lists the attributes the drive's *own* manufacturer thresholds call failed.
//
// Selected by the drive's threshold rather than by this app's, and that is the whole point. An RMA
// conversation turns on "attribute 5 is at or below its own threshold" — a limit the vendor wrote
// into the drive, in the drive's own terms — and not on a score this app computed. It also means
// this list cannot contradict the stored verdict: it is a listing rather than a verdict, so a
// threshold the user changed since the scan cannot make the report disagree with itself.
//
// A threshold of zero means "no threshold", not "must be zero". Plenty of attributes report 0
// there, and listing every one of them would bury the real findings in noise.
func failingAttributes(attrs []model.Attribute) []model.Attribute {
	var out []model.Attribute
	for _, a := range attrs {
		if a.Threshold > 0 && a.Value <= a.Threshold {
			out = append(out, a)
		}
	}
	return out
}

// evidenceCounters lists the attributes the app reads as evidence whose raw count is non-zero.
//
// The ids come from `interp.EvidenceAttributes`, and the count is read by the interpreter itself,
// so the report names exactly the attributes the verdict reads and reads them the same way. A zero
// is dropped rather than listed: the absence of a counter is not a finding, and a table of zeros
// reads as a clean bill of health the app has not issued.
func evidenceCounters(attrs []model.Attribute) []model.Attribute {
	byID := make(map[int]model.Attribute, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	var out []model.Attribute
	for _, ev := range interp.EvidenceAttributes() {
		a, ok := byID[ev.ID]
		if !ok || interp.AttributeCount(a) == 0 {
			continue
		}
		if strings.TrimSpace(a.Name) == "" {
			a.Name = ev.Label
		}
		out = append(out, a)
	}
	return out
}

// renderTextReport produces a CrystalDiskInfo-style plain-text report suitable
// for a warranty claim (docs/FEATURES.md A8).
func renderTextReport(d store.DeviceRecord, snap *store.Snapshot) string {
	payload := decodeSnapshot(snap)

	var b strings.Builder
	fmt.Fprintf(&b, "smart-serve report\n==================\n\n")
	fmt.Fprintf(&b, "%s\n\n", wrapText(reportPurpose, 78))
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

	writeTextEvidence(&b, payload)
	fmt.Fprintf(&b, "\n%s\n", wrapText(reportNote, 78))
	return b.String()
}

// writeTextEvidence appends the two evidence blocks to a plain-text report.
//
// This is the part that makes the export an RMA document rather than a summary. A vendor asks for
// the counters, and the text report — the default format, and the one people paste into a support
// ticket — had no attribute table at all. A section with no findings is omitted rather than printed
// empty, because "nothing here" and "we did not look" are different, and an empty table reads as
// the second.
func writeTextEvidence(b *strings.Builder, payload snapshotPayload) {
	if failed := failingAttributes(payload.Attributes); len(failed) > 0 {
		fmt.Fprintf(b, "\nFailing attributes (at or below the drive's own threshold)\n")
		for _, a := range failed {
			fmt.Fprintf(b, "  %s\n", attributeLine(a))
		}
	}
	if counters := evidenceCounters(payload.Attributes); len(counters) > 0 {
		fmt.Fprintf(b, "\nEvidence counters (non-zero, as this app reads them)\n")
		for _, a := range counters {
			fmt.Fprintf(b, "  %s\n", counterLine(a))
		}
	}
}

// attributeLine renders one attribute for a plain-text report.
//
// The normalised value, worst and threshold are all included because they are what the drive's own
// threshold rule is expressed in: an RMA needs the comparison, not only the conclusion. `rawText`
// prefers smartctl's own rendering, which is the number a support engineer will have seen.
func attributeLine(a model.Attribute) string {
	return fmt.Sprintf("%3d  %-26s value %3d  worst %3d  threshold %3d  raw %s",
		a.ID, a.Name, a.Value, a.Worst, a.Threshold, rawText(a))
}

// counterLine renders one evidence counter: its id, its name and its count.
//
// Deliberately not the full attribute line. The failing-attribute section above is about a
// *comparison* — the value against the drive's own threshold — and this one is about a number, so
// printing the comparison twice would be noise on exactly the drives where the reader is reading
// hardest. An attribute that appears in both appears in both because it is two facts, not because it
// was duplicated.
func counterLine(a model.Attribute) string {
	return fmt.Sprintf("%3d  %-26s %s", a.ID, a.Name, rawText(a))
}

// wrapText folds a paragraph to a column, so a plain-text report stays readable in a terminal, an
// email and a PDF print without anyone reflowing it by hand.
func wrapText(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	line := 0
	for i, w := range words {
		if i > 0 {
			if line+1+len(w) > width {
				b.WriteString("\n")
				line = 0
			} else {
				b.WriteString(" ")
				line++
			}
		}
		b.WriteString(w)
		line += len(w)
	}
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
	".purpose{border-left:3px solid #d8dee4;padding-left:.75rem;color:#3d444d;margin:1rem 0 1.5rem}" +
	".cap{color:#59636e;font-size:.85rem;margin:-.25rem 0 .5rem}" +
	"footer{margin-top:2rem;color:#59636e;font-size:.75rem;border-top:1px solid #d8dee4;padding-top:.75rem}"

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
	b.WriteString("<p class=\"purpose\">" + html.EscapeString(reportPurpose) + "</p>\n")

	b.WriteString("<h2>Summary</h2>\n<dl>\n")
	for _, f := range reportFacts(d, snap) {
		if strings.TrimSpace(f.value) == "" {
			continue
		}
		fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>\n", html.EscapeString(f.label), html.EscapeString(f.value))
	}
	b.WriteString("</dl>\n")

	// The evidence blocks come first, because on a failing drive they are what the reader is here
	// for; the full attribute dump below is the appendix.
	writeHTMLAttributes(&b, "Failing attributes",
		"At or below the threshold the drive's own manufacturer set. This is the drive's own rule, "+
			"not this app's.",
		failingAttributes(payload.Attributes))
	writeHTMLCounters(&b, "Evidence counters",
		"The counters this app reads when it assesses a drive, where the raw value is non-zero.",
		evidenceCounters(payload.Attributes))
	writeHTMLAttributes(&b, "S.M.A.R.T. attributes", "", payload.Attributes)

	if n := payload.NVMe; n != nil {
		b.WriteString("<h2>NVMe health log</h2>\n<dl>\n")
		for _, f := range nvmeFacts(n) {
			fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>\n", html.EscapeString(f.label), html.EscapeString(f.value))
		}
		b.WriteString("</dl>\n")
	}

	b.WriteString("<footer>" + html.EscapeString(reportNote) + "</footer>\n")
	b.WriteString("</body></html>\n")
	return b.String()
}

// writeHTMLAttributes renders one attribute table for the exported page.
//
// Omitted entirely when there is nothing to list: an empty table reads as "we looked and found
// nothing", and the honest reading of no findings is that there were none to report.
func writeHTMLAttributes(b *strings.Builder, title, caption string, attrs []model.Attribute) {
	if len(attrs) == 0 {
		return
	}
	fmt.Fprintf(b, "<h2>%s</h2>\n<p class=\"cap\">%s</p>\n<table>\n<thead><tr>"+
		"<th>ID</th><th>Attribute</th><th>Value</th><th>Worst</th><th>Thresh</th><th>Raw</th>"+
		"</tr></thead>\n<tbody>\n", html.EscapeString(title), html.EscapeString(caption))
	for _, a := range attrs {
		fmt.Fprintf(b, "<tr><td class=\"num\">%d</td><td>%s</td>"+
			"<td class=\"num\">%d</td><td class=\"num\">%d</td><td class=\"num\">%d</td>"+
			"<td class=\"num\">%s</td></tr>\n",
			a.ID, html.EscapeString(a.Name), a.Value, a.Worst, a.Threshold, html.EscapeString(rawText(a)))
	}
	b.WriteString("</tbody></table>\n")
}

// writeHTMLCounters renders the evidence-counter table: the id, the name and the count.
//
// Three columns rather than the attribute table's six, and that is the point of the section. The
// failing-attribute table above is about a comparison against the drive's own threshold; this one is
// about a number. An attribute in both is in both because it is two facts, not because it was
// duplicated, and printing the same six cells twice would say otherwise.
func writeHTMLCounters(b *strings.Builder, title, caption string, attrs []model.Attribute) {
	if len(attrs) == 0 {
		return
	}
	fmt.Fprintf(b, "<h2>%s</h2>\n<p class=\"cap\">%s</p>\n<table>\n<thead><tr>"+
		"<th>ID</th><th>Attribute</th><th>Raw</th></tr></thead>\n<tbody>\n",
		html.EscapeString(title), html.EscapeString(caption))
	for _, a := range attrs {
		fmt.Fprintf(b, "<tr><td class=\"num\">%d</td><td>%s</td><td class=\"num\">%s</td></tr>\n",
			a.ID, html.EscapeString(a.Name), html.EscapeString(rawText(a)))
	}
	b.WriteString("</tbody></table>\n")
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
	// The purpose first, so the file explains itself to whoever opens it — including the support
	// engineer it gets forwarded to, who has no reason to know what generated it.
	write("Purpose", reportPurpose)
	for _, f := range reportFacts(d, snap) {
		write(f.label, f.value)
	}

	// The evidence, in the two groups the other formats use. An RMA is an argument about specific
	// counters, so they are named rather than left to be found among the raw attribute dump below.
	for _, a := range failingAttributes(payload.Attributes) {
		write(fmt.Sprintf("Failed attribute %d (%s)", a.ID, a.Name),
			fmt.Sprintf("raw %s — value %d at or below its own threshold of %d",
				rawText(a), a.Value, a.Threshold))
	}
	for _, a := range evidenceCounters(payload.Attributes) {
		write(fmt.Sprintf("Counter %d (%s)", a.ID, a.Name), rawText(a))
	}

	for _, a := range payload.Attributes {
		write(fmt.Sprintf("attr %d %s", a.ID, a.Name), rawText(a))
	}
	if n := payload.NVMe; n != nil {
		for _, f := range nvmeFacts(n) {
			write("nvme "+f.label, f.value)
		}
	}
	write("Note", reportNote)
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
	return smartStateText(d.SmartAvailable, d.SmartError)
}

// smartStateText says whether a drive reported S.M.A.R.T. at all.
//
// This is the field that explains the blanks. A drive with no S.M.A.R.T. leaves every reading empty,
// and without it a reader concludes the export is broken rather than that the drive reports nothing
// — the same mistake the em dash prevents on screen.
func smartStateText(available bool, errMsg string) string {
	if available {
		return "available"
	}
	if errMsg != "" {
		return "unavailable (" + errMsg + ")"
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
