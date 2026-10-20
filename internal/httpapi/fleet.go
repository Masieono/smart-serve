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
	"github.com/masieono/smart-serve/internal/store"
)

// fleetPurpose is what the fleet export is for (`FEATURES.md` A23).
//
// Two audiences, and one file has to serve both: the spreadsheet you sort when planning a purchase,
// and the page you print or attach when several drives are going back at once. Both are documents
// someone else reads, so both say what they are.
const fleetPurpose = "Every drive this host has seen, with its identity, its health and the readings " +
	"that predict it — a fleet inventory for planning a purchase, and warranty / RMA evidence for " +
	"drives that are going back. The data is read from the drives by smartmontools and is not edited."

// fleetRow is one drive's line in the fleet export: the payload the dashboard shows, plus the
// ranking's own words for it.
//
// The payload is embedded rather than copied field by field, so a new field on the device list is
// available to the export without a second place to remember. The band and its reasons come from
// the replacement ranking (`A24`) rather than being recomputed here: the export has to say what the
// dashboard says, and a second implementation of the banding rule is a second answer to "which one
// first?".
type fleetRow struct {
	deviceJSON
	Band    string
	Reasons string
}

// handleExportFleet serves the whole fleet as one file (`FEATURES.md` A23).
//
// The route is `/api/devices/export`, which shadows a device whose serial is literally "export".
// The mux resolves it deterministically — a literal path segment is more specific than a wildcard —
// so the only cost is that such a serial is unreachable, and no drive has one.
func (s *Server) handleExportFleet(w http.ResponseWriter, r *http.Request) {
	devices, ranks, err := s.deviceRows(r.Context())
	if err != nil {
		s.fail(w, "export fleet", err)
		return
	}

	// The filename carries the scope, because the filename is what the file is called in the folder
	// it is saved to: a batch's sheet and the whole fleet's must not land on top of each other.
	name := "smart-serve-fleet"
	if tag := strings.TrimSpace(r.URL.Query().Get("tag")); tag != "" {
		devices, ranks = withTag(devices, ranks, tag)
		name += "-" + safeFilename(tag)
	}

	rows := fleetRows(devices, ranks)
	stamp := time.Now().UTC().Format("2006-01-02")

	switch format := r.URL.Query().Get("format"); format {
	case "", "csv":
		downloadAs(w, "text/csv; charset=utf-8", name+"-"+stamp+".csv")
		_, _ = fmt.Fprint(w, renderFleetCSV(rows))
	case "html":
		downloadAs(w, "text/html; charset=utf-8", name+"-"+stamp+".html")
		_, _ = fmt.Fprint(w, renderFleetHTML(rows, stamp))
	default:
		// No `json`. `/api/devices` already is the JSON, and a second shape would be a second thing
		// to keep in step for no reader who wants it.
		writeError(w, http.StatusBadRequest, "unsupported format (want csv or html)")
	}
}

// withTag narrows the export to the drives carrying one tag (`FEATURES.md` D11).
//
// This is the batch's own file. "Report the batch" needs one artefact, and the alternative is
// exporting the whole fleet and filtering by the Tags column in the spreadsheet.
//
// A **deliberate** scope, which is the only reason it can exist at all beside the rule that the
// export ignores the dashboard's filter. The two are different things: one is the caller asking for
// a subset, the other is view state the caller did not ask for and would not notice. The ranking is
// filtered with the devices rather than recomputed, so the batch keeps the order the fleet had.
func withTag(devices []deviceJSON, ranks []interp.ReplacementRank, tag string) ([]deviceJSON, []interp.ReplacementRank) {
	kept := make([]deviceJSON, 0, len(devices))
	inScope := make(map[string]bool, len(devices))
	for _, d := range devices {
		if !store.HasTag(d.Tags, tag) {
			continue
		}
		kept = append(kept, d)
		inScope[d.Serial] = true
	}

	keptRanks := make([]interp.ReplacementRank, 0, len(ranks))
	for _, rank := range ranks {
		if inScope[rank.Serial] {
			keptRanks = append(keptRanks, rank)
		}
	}
	return kept, keptRanks
}

// fleetRows pairs the list payloads with the ranking and puts them in the ranking's own order.
//
// The order is the one thing a reader cannot recover from the file. Sorting by the band column
// gives fine, now, soon, watch — alphabetical, and meaningless — and reconstructing "worst first"
// needs the banding rule itself. Walking the ranking keeps the export in the order the app would
// answer in.
//
// Disconnected drives come last and unranked. The ranking leaves them out on purpose (`A24`: it is
// a finding of its own, and ranking a backup drive someone unplugged would put it above a drive
// that is actually failing), but an inventory has to include them, so they are appended rather than
// dropped. Their band stays empty rather than reading "fine", because "fine" is a claim the ranking
// did not make about them.
func fleetRows(devices []deviceJSON, ranks []interp.ReplacementRank) []fleetRow {
	bySerial := make(map[string]deviceJSON, len(devices))
	for _, d := range devices {
		bySerial[d.Serial] = d
	}

	rows := make([]fleetRow, 0, len(devices))
	ranked := make(map[string]bool, len(devices))
	for _, rank := range ranks {
		d, ok := bySerial[rank.Serial]
		if !ok {
			continue
		}
		ranked[rank.Serial] = true
		rows = append(rows, fleetRow{
			deviceJSON: d,
			Band:       string(rank.Band),
			Reasons:    strings.Join(rank.Reasons, "; "),
		})
	}
	for _, d := range devices {
		if !ranked[d.Serial] {
			rows = append(rows, fleetRow{deviceJSON: d})
		}
	}
	return rows
}

// fleetColumn is one column of the fleet export, shared by the CSV and the printable page so the
// two can never disagree about what a row says.
type fleetColumn struct {
	header  string
	numeric bool
	value   func(fleetRow) string
}

// fleetColumns is the export's shape, in the order someone reads a fleet: where it is, what it is,
// how it is, how old it is, and what to do about it.
//
// It is deliberately not the dashboard table's columns. A screen shows a dozen cells and hides the
// rest behind a drive's page; a spreadsheet is where you compare twenty drives at once, so the
// columns that are too noisy to sit on a card — wear, new sector problems, the ranking's reasons —
// belong here.
func fleetColumns() []fleetColumn {
	return []fleetColumn{
		{header: "Bay", value: func(r fleetRow) string { return r.Bay }},
		{header: "Drive", value: func(r fleetRow) string { return firstNonEmpty(r.Name, r.Model) }},
		{header: "Serial", value: func(r fleetRow) string { return r.Serial }},
		{header: "Model", value: func(r fleetRow) string { return r.Model }},
		{header: "Firmware", value: func(r fleetRow) string { return r.Firmware }},
		{header: "Capacity", value: func(r fleetRow) string { return humanBytes(r.CapacityBytes) }},
		{header: "Transport", value: func(r fleetRow) string { return r.Transport }},
		{header: "Verdict", value: func(r fleetRow) string { return strings.ToUpper(r.Health) }},
		{header: "Score", numeric: true, value: func(r fleetRow) string { return fleetScore(r.Score) }},
		{header: "Replace", value: func(r fleetRow) string { return r.Band }},
		{header: "Why", value: func(r fleetRow) string { return r.Reasons }},
		{header: "Temp C", numeric: true, value: func(r fleetRow) string { return fleetInt(r.TemperatureC) }},
		{header: "Power on h", numeric: true, value: func(r fleetRow) string { return fleetInt64(r.PowerOnHours) }},
		{header: "Wear %", numeric: true, value: func(r fleetRow) string { return fleetPct(r.WearPct) }},
		{header: "New sectors", numeric: true, value: func(r fleetRow) string { return fleetInt64(r.Worsening) }},
		{header: "Tags", value: func(r fleetRow) string { return r.Tags }},
		{header: "First seen", value: func(r fleetRow) string { return r.FirstSeen }},
		{header: "Last scan", value: func(r fleetRow) string { return fleetString(r.LastScan) }},
		{header: "Connected", value: func(r fleetRow) string { return yesNo(!r.IsMissing) }},
		{header: "Hidden", value: func(r fleetRow) string { return yesNo(r.Hidden) }},
		{header: "S.M.A.R.T.", value: func(r fleetRow) string {
			return smartStateText(r.SmartAvailable, r.SmartError)
		}},
	}
}

// fleetScore renders a score, or an empty cell for a drive that was never scored.
//
// Empty rather than zero, and it matters more here than anywhere else in the app. A blank cell is
// what a spreadsheet's AVERAGE and MIN skip, while a 0 is a value: it drags every average down and
// reads as "this drive scored zero", which is a claim the app is not making. `-1` is how the API
// spells "not scored" (`API.md`), and it means exactly that — no reading, not a bad one.
func fleetScore(score int) string {
	if score < 0 {
		return ""
	}
	return strconv.Itoa(score)
}

// fleetInt, fleetInt64, fleetPct and fleetString render an absent reading as an empty cell, never a
// zero.
//
// The same rule as the em dash on screen (`UI.md`), and it has teeth here because this is the file
// the numbers get aggregated in: "0 °C" and "0 hours" are claims about a drive, and a blank is not.
func fleetInt(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func fleetInt64(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func fleetPct(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.0f", *v)
}

func fleetString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// renderFleetCSV renders the fleet as a spreadsheet.
//
// **One row per drive, not one row per attribute.** A spreadsheet's job here is to compare drives
// with each other — oldest first, most worn first, which one to buy — and a field/value sheet cannot
// be sorted by anything. The per-attribute sheet is the single-drive report's job (`A8`).
//
// **The file introduces itself.** Two lines above the header say what it is, because a CSV in a
// shared folder gets opened months later by someone with no memory of where it came from, and the
// column headers alone do not say whether this is an inventory or a failure report. The cost is that
// a strict parser sees a preamble; the machine-readable form is `/api/devices`, which already exists.
func renderFleetCSV(rows []fleetRow) string {
	cols := fleetColumns()

	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"smart-serve fleet export"})
	_ = w.Write([]string{fleetPurpose})
	// A blank line separates the preamble from the header, which is what a reader scrolling a
	// spreadsheet expects and what a CSV reader skips.
	_ = w.Write([]string{})

	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.header
	}
	_ = w.Write(header)

	for _, row := range rows {
		record := make([]string, len(cols))
		for i, c := range cols {
			record[i] = c.value(row)
		}
		_ = w.Write(record)
	}
	w.Flush()
	return b.String()
}

// driveCount is the fleet's size as a phrase, so a one-drive export does not read as a template
// nobody finished.
func driveCount(n int) string {
	if n == 1 {
		return "1 drive"
	}
	return fmt.Sprintf("%d drives", n)
}

// fleetCSS is the printable page's own styling, on top of the report's.
//
// Twenty-one columns only fit a sheet of paper sideways, so the print rule says so rather than
// leaving the browser to guess and clip the last columns — the ones the page exists for.
const fleetCSS = reportCSS +
	"body{margin:1.5rem}h1{font-size:1.3rem}" +
	"table{font-size:.78rem}th,td{padding:.25rem .4rem;white-space:nowrap}" +
	"tr.missing td{color:#59636e}" +
	"@media print{@page{size:landscape;margin:10mm}}"

// renderFleetHTML renders the fleet as one printable page (`FEATURES.md` A23).
//
// The same rows as the CSV, through the same column list, so the page and the file cannot disagree.
// It covers the case the CSV does not: several drives going back at once, where the argument is
// "these all came from the same batch" and a printed sheet with a heading on it is what gets
// attached to the claim.
func renderFleetHTML(rows []fleetRow, stamp string) string {
	cols := fleetColumns()

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>smart-serve fleet export " + html.EscapeString(stamp) + "</title>\n")
	b.WriteString("<style>" + fleetCSS + "</style></head><body>\n")

	b.WriteString("<h1>Fleet export</h1>\n")
	fmt.Fprintf(&b, "<div class=\"sub\">%s · exported %s</div>\n",
		driveCount(len(rows)), html.EscapeString(stamp))
	b.WriteString("<p class=\"purpose\">" + html.EscapeString(fleetPurpose) + "</p>\n")

	if len(rows) == 0 {
		// Said outright. An empty table reads as a page that failed to render, not as a host with
		// no drives on it — and the second is a real answer.
		b.WriteString("<p>No drives have been recorded yet. Run a scan and export again.</p>\n")
	} else {
		b.WriteString("<table>\n<thead><tr>")
		for _, c := range cols {
			b.WriteString("<th" + numClass(c.numeric) + ">" + html.EscapeString(c.header) + "</th>")
		}
		b.WriteString("</tr></thead>\n<tbody>\n")
		for _, row := range rows {
			class := ""
			if row.IsMissing {
				class = " class=\"missing\""
			}
			b.WriteString("<tr" + class + ">")
			for _, c := range cols {
				b.WriteString("<td" + numClass(c.numeric) + ">" + html.EscapeString(c.value(row)) + "</td>")
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody></table>\n")
	}

	b.WriteString("<footer>" + html.EscapeString(reportNote) + "</footer>\n")
	b.WriteString("</body></html>\n")
	return b.String()
}

func numClass(numeric bool) string {
	if numeric {
		return " class=\"num\""
	}
	return ""
}
