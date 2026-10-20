package httpapi

import (
	"context"
	"encoding/csv"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// fleetCSV parses an exported fleet CSV into its header and its data rows, skipping the preamble.
//
// `FieldsPerRecord` is disabled because the preamble's rows are one cell wide and the data rows are
// twenty-one, which is exactly the shape a strict reader would reject.
func fleetCSV(t *testing.T, body string) ([]string, [][]string) {
	t.Helper()
	r := csv.NewReader(strings.NewReader(body))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v\n%s", err, body)
	}
	for i, rec := range records {
		if len(rec) > 1 && rec[0] == "Bay" {
			return rec, records[i+1:]
		}
	}
	t.Fatalf("no header row in:\n%s", body)
	return nil, nil
}

// cell reads one column out of a row by its header name.
func cell(t *testing.T, header, row []string, name string) string {
	t.Helper()
	i := slices.Index(header, name)
	if i < 0 {
		t.Fatalf("no %q column in %v", name, header)
	}
	if i >= len(row) {
		return ""
	}
	return row[i]
}

func TestFleetExportHasOneRowPerDrive(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodGet, "/api/devices/export", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content type = %q, want CSV", ct)
	}
	// The filename is part of the document: it is what the file is called in the folder it is saved
	// to, beside a dozen other exports.
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "smart-serve-fleet-") ||
		!strings.Contains(cd, ".csv") {
		t.Errorf("content disposition = %q, want a dated .csv filename", cd)
	}

	header, rows := fleetCSV(t, rec.Body.String())
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1:\n%s", len(rows), rec.Body.String())
	}
	if got := cell(t, header, rows[0], "Serial"); got != "S1" {
		t.Errorf("serial = %q, want S1", got)
	}
	// The model is what a purchase plan is matched against and the capacity is what it is compared
	// on, so both belong in the row rather than only on a drive's page.
	if got := cell(t, header, rows[0], "Model"); got != "Samsung SSD 870 EVO" {
		t.Errorf("model = %q", got)
	}
	if got := cell(t, header, rows[0], "Capacity"); got != "1.0 TB" {
		t.Errorf("capacity = %q, want 1.0 TB", got)
	}
}

// The export is the inventory, not the view. A file that silently contained a subset would be the
// one mistake nobody catches: a wrong number gets noticed, a missing row does not.
func TestTheFleetExportIsNotFiltered(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	if err := st.SetNotes(context.Background(), store.Notes{Serial: "S1", Hidden: true}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/devices/export", "", nil)
	header, rows := fleetCSV(t, rec.Body.String())
	if len(rows) != 1 {
		t.Fatalf("a hidden drive was dropped from the export: %d rows", len(rows))
	}
	// The column is what explains why it is not on screen. Without it the reader concludes the
	// export is broken.
	if got := cell(t, header, rows[0], "Hidden"); got != "yes" {
		t.Errorf("Hidden = %q, want yes", got)
	}
}

// A blank cell is what a spreadsheet's AVERAGE and MIN skip; a zero is a value, and it reads as a
// measurement. "0 °C" is a claim about a drive, and a blank is not.
func TestAnAbsentReadingIsAnEmptyCellNotAZero(t *testing.T) {
	temp, hours, wear := 34, int64(1234), 12.0
	rows := []fleetRow{
		{deviceJSON: deviceJSON{Serial: "NONE", Health: "unknown", Score: -1}},
		{deviceJSON: deviceJSON{
			Serial: "SOME", Health: "good", Score: 100,
			TemperatureC: &temp, PowerOnHours: &hours, WearPct: &wear,
		}},
	}

	header, records := fleetCSV(t, renderFleetCSV(rows))
	if len(records) != 2 {
		t.Fatalf("got %d rows, want 2", len(records))
	}

	for _, col := range []string{"Score", "Temp C", "Power on h", "Wear %", "New sectors"} {
		if got := cell(t, header, records[0], col); got != "" {
			t.Errorf("%s = %q for a drive that reported nothing, want an empty cell", col, got)
		}
	}
	// The other half, so the test cannot pass by rendering everything blank.
	if got := cell(t, header, records[1], "Score"); got != "100" {
		t.Errorf("Score = %q, want 100", got)
	}
	if got := cell(t, header, records[1], "Temp C"); got != "34" {
		t.Errorf("Temp C = %q, want 34", got)
	}
	if got := cell(t, header, records[1], "Wear %"); got != "12" {
		t.Errorf("Wear %% = %q, want 12", got)
	}
}

// The ranking leaves disconnected drives out (`A24`), and "fine" would be a claim it did not make
// about them. An inventory still has to list them.
func TestADisconnectedDriveIsListedButNotRanked(t *testing.T) {
	rows := fleetRows(
		[]deviceJSON{{Serial: "GONE", Health: "good", Score: 90, IsMissing: true}},
		interp.RankForReplacement(nil),
	)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want the disconnected drive included", len(rows))
	}
	if rows[0].Band != "" {
		t.Errorf("band = %q, want empty rather than \"fine\"", rows[0].Band)
	}

	header, records := fleetCSV(t, renderFleetCSV(rows))
	if got := cell(t, header, records[0], "Connected"); got != "no" {
		t.Errorf("Connected = %q, want no", got)
	}
}

// The order is the one thing a reader cannot recover from the file: sorting the band column
// alphabetically gives fine, now, soon, watch, which is meaningless, and reconstructing "worst
// first" needs the banding rule itself.
func TestTheExportKeepsTheRankingOrder(t *testing.T) {
	devices := []deviceJSON{
		{Serial: "OK", Health: "good", Score: 100},
		{Serial: "BAD", Health: "bad", Score: 10},
	}
	ranks := interp.RankForReplacement([]interp.ReplacementCandidate{
		{Serial: "OK", Health: model.Good, Score: 100},
		{Serial: "BAD", Health: model.Bad, Rationale: "5 reallocated sectors", Score: 10},
	})

	rows := fleetRows(devices, ranks)
	if rows[0].Serial != "BAD" {
		t.Errorf("first row = %q, want the failing drive first", rows[0].Serial)
	}
	// The reasons are the column a forum post quotes, so the export carries them rather than leaving
	// the reader to work out what put the drive in its band.
	if !strings.Contains(rows[0].Reasons, "reallocated") {
		t.Errorf("reasons = %q, want the drive's own words", rows[0].Reasons)
	}
}

func TestTheFleetExportSaysWhatItIs(t *testing.T) {
	body := renderFleetCSV(nil)
	if !strings.Contains(body, "smart-serve fleet export") {
		t.Errorf("no title in:\n%s", body)
	}
	if !strings.Contains(body, "fleet inventory") {
		t.Errorf("no purpose in:\n%s", body)
	}

	// An empty table reads as a page that failed to render, not as a host with no drives on it —
	// and the second is a real answer.
	page := renderFleetHTML(nil, "2025-10-12")
	if !strings.Contains(page, "No drives have been recorded yet") {
		t.Errorf("an empty export does not say so:\n%s", page)
	}
}

func TestFleetExportRejectsAnUnknownFormat(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodGet, "/api/devices/export?format=pdf", "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// The export route shadows a device whose serial is literally "export". The mux resolves a literal
// segment ahead of a wildcard, so what is worth checking is that the ordinary routes still work.
func TestTheExportRouteDoesNotShadowDeviceRoutes(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	for _, path := range []string{"/api/devices/S1", "/api/devices/S1/report"} {
		if rec := do(t, srv, http.MethodGet, path, "", nil); rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, rec.Code)
		}
	}
}

// The batch's own file (`FEATURES.md` D11). A scope the caller asked for, which is the only reason it
// can exist beside the rule that the export ignores the dashboard's filter: view state the caller did
// not ask for is a different thing from a parameter they did.
func TestTheExportCanBeScopedToATag(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)
	if err := st.SetNotes(context.Background(), store.Notes{Serial: "S1", Tags: "bench, spare"}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/devices/export?tag=spare", "", nil)
	header, rows := fleetCSV(t, rec.Body.String())
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want the one tagged drive", len(rows))
	}
	if got := cell(t, header, rows[0], "Serial"); got != "S1" {
		t.Errorf("serial = %q", got)
	}
	// The filename carries the scope, because the filename is what the file is called in the folder
	// it is saved to: a batch's sheet and the whole fleet's must not land on top of each other.
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "spare") {
		t.Errorf("content disposition = %q, want the tag in the filename", cd)
	}

	// Case-insensitively, because the tag was typed by hand and the filter should match the way the
	// grouping does.
	rec = do(t, srv, http.MethodGet, "/api/devices/export?tag=SPARE", "", nil)
	if _, rows := fleetCSV(t, rec.Body.String()); len(rows) != 1 {
		t.Errorf("got %d rows for a tag in the wrong case", len(rows))
	}

	// A tag nothing carries is an empty sheet, not an error: "the batch is empty" is a real answer,
	// and a 404 would send the reader looking for a fault that is not there.
	rec = do(t, srv, http.MethodGet, "/api/devices/export?tag=nope", "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d for a tag nothing carries, want 200", rec.Code)
	}
	if _, rows := fleetCSV(t, rec.Body.String()); len(rows) != 0 {
		t.Errorf("got %d rows for a tag nothing carries", len(rows))
	}
}

// A drive on the bench is being evaluated, not used (`FEATURES.md` D11). Ranking it would put a drive
// that is *expected* to fail above one in service that is actually failing — the same mistake the
// disconnected rule avoids, in the other direction.
func TestABenchDriveIsListedButNotRanked(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)
	if err := st.SetNotes(context.Background(), store.Notes{Serial: "S1", Tags: "Bench"}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	var body struct {
		Devices  []deviceJSON             `json:"devices"`
		Ranking  []interp.ReplacementRank `json:"ranking"`
		BenchTag string                   `json:"bench_tag"`
	}
	decodeBody(t, do(t, srv, http.MethodGet, "/api/devices", "", nil), &body)

	if len(body.Devices) != 1 {
		t.Fatalf("got %d devices, want the bench drive still listed", len(body.Devices))
	}
	// The server decides which drives are on the bench, so the UI never has to know the tag's name —
	// or split a tag string the way the grouping does.
	if !body.Devices[0].OnBench {
		t.Error("on_bench is not set for a drive tagged Bench")
	}
	if body.BenchTag != store.BenchTag {
		t.Errorf("bench_tag = %q, want %q", body.BenchTag, store.BenchTag)
	}
	// Healthy, so it would otherwise be ranked "fine" and appear in the shortlist's data.
	for _, r := range body.Ranking {
		if r.Serial == "S1" {
			t.Errorf("the bench drive is in the ranking: %+v", r)
		}
	}
}
