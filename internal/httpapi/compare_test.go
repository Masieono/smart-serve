package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// The alignment is the feature, so it is tested as a pure function rather than only through the
// endpoint. Every case here is a way a side-by-side table goes wrong: rows in a different order for
// different drives, a missing attribute reading as a zero, or the "these disagree" mark being
// useless because it is always on.
func TestAlignAttributes(t *testing.T) {
	// Two drives of one model, reporting the same attributes in different orders — which is what
	// smartctl actually does, and the reason this cannot be done in the browser.
	first := []model.Attribute{
		{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100, Worst: 100, Threshold: 10, RawValue: 0},
		{ID: 9, Name: "Power_On_Hours", Value: 98, Worst: 98, RawValue: 1200},
		{ID: 194, Name: "Temperature_Celsius", Value: 35, Worst: 50, RawValue: 35},
	}
	second := []model.Attribute{
		{ID: 9, Name: "Power_On_Hours", Value: 97, Worst: 97, RawValue: 4300},
		{ID: 194, Name: "Temperature_Celsius", Value: 41, Worst: 50, RawValue: 41},
		{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100, Worst: 100, Threshold: 10, RawValue: 12},
	}

	rows := alignAttributes([][]model.Attribute{first, second})

	// Ordered by attribute id, which is the one order every drive agrees on.
	wantOrder := []int{5, 9, 194}
	if len(rows) != len(wantOrder) {
		t.Fatalf("rows = %d, want %d", len(rows), len(wantOrder))
	}
	for i, id := range wantOrder {
		if rows[i].ID != id {
			t.Errorf("row %d has id %d, want %d", i, rows[i].ID, id)
		}
		if len(rows[i].Cells) != 2 {
			t.Fatalf("row %d has %d cells, want one per drive", i, len(rows[i].Cells))
		}
	}

	// A cell's position is its column.
	if got := rows[0].Cells[1].RawValue; got != 12 {
		t.Errorf("drive 2's reallocated raw = %d, want 12", got)
	}

	// The sector count differs in the *raw* count while both normalised values are 100. Comparing
	// values alone would call these two drives identical, which is the finding the table exists for.
	if !rows[0].Differs {
		t.Error("the reallocated-sector row should differ: 0 raw vs 12 raw")
	}
	if !rows[1].Differs {
		t.Error("power-on hours should differ: 1200 vs 4300")
	}
	if !rows[2].Differs {
		t.Error("temperature should differ: 35 vs 41")
	}

	// The drive's own alarm, not this app's opinion: a threshold of 10 with a value of 100 is fine.
	if rows[0].Flagged {
		t.Error("nothing should be flagged on a healthy pair")
	}
}

// A drive that does not have an attribute is *absent*, not zero. An NVMe drive has no ATA
// attributes, and a table that filled the column with zeros would be inventing readings.
func TestAlignedAttributesMarkAbsence(t *testing.T) {
	sata := []model.Attribute{{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100, Threshold: 10}}

	rows := alignAttributes([][]model.Attribute{sata, {}})

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Cells[0] == nil {
		t.Fatal("the SATA drive's cell should be present")
	}
	if rows[0].Cells[1] != nil {
		t.Errorf("the NVMe drive's cell should be absent, got %+v", rows[0].Cells[1])
	}
	// One present value cannot disagree with anything.
	if rows[0].Differs {
		t.Error("a row only one drive has does not differ — absence is not a difference")
	}
}

// The drive's own threshold is what flags an attribute, and a threshold of zero means *no
// threshold* rather than *fails at zero* — the mistake that would flag every attribute on a drive
// that publishes zeros.
func TestAlignedAttributesFlagTheDrivesOwnThreshold(t *testing.T) {
	rows := alignAttributes([][]model.Attribute{
		{{ID: 5, Name: "Reallocated_Sector_Ct", Value: 5, Threshold: 10, RawValue: 900}},
		{{ID: 9, Name: "Power_On_Hours", Value: 98, RawValue: 1200}},
	})

	if !rows[0].Flagged {
		t.Error("a value at or below the drive's own threshold should be flagged")
	}
	if rows[1].Flagged {
		t.Error("a threshold of 0 means no threshold, not a drive that fails at zero")
	}
}

// The row's name comes from the first drive that named it: two drives can disagree about an
// attribute's name, and one name with the disagreement marked is more useful than two rows for one
// id.
func TestAlignedAttributesTakeTheFirstName(t *testing.T) {
	rows := alignAttributes([][]model.Attribute{
		{{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100}},
		{{ID: 5, Name: "Reallocated_Event_Count", Value: 100}},
	})

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want one row for one id", len(rows))
	}
	if rows[0].Name != "Reallocated_Sector_Ct" {
		t.Errorf("name = %q, want the first drive's name", rows[0].Name)
	}
}

func TestCompareSerials(t *testing.T) {
	good := []struct {
		raw  string
		want []string
	}{
		{"a,b", []string{"a", "b"}},
		{" a , b ", []string{"a", "b"}},
		// The order the caller gave is the order of the columns.
		{"b,a", []string{"b", "a"}},
		// Comparing a drive with itself is a mistake worth absorbing.
		{"a,a,b", []string{"a", "b"}},
		// Blanks are not drives.
		{"a,,b,", []string{"a", "b"}},
	}
	for _, tc := range good {
		got, err := compareSerials(tc.raw)
		if err != nil {
			t.Errorf("compareSerials(%q) = %v, want no error", tc.raw, err)
			continue
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("compareSerials(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}

	bad := []struct {
		raw  string
		want string
	}{
		{"", "name the drives"},
		{"   ", "name the drives"},
		{"a", "at least two"},
		{"a,b,c,d,e,f,g", "at most"},
	}
	for _, tc := range bad {
		_, err := compareSerials(tc.raw)
		if err == nil {
			t.Errorf("compareSerials(%q) = nil, want an error", tc.raw)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("compareSerials(%q) = %q, want it to mention %q", tc.raw, err, tc.want)
		}
	}
}

// The endpoint, end to end: two stored drives with an attribute blob each.
func TestCompareEndpoint(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	saveComparisonFixtures(t, st)

	rec := do(t, srv, http.MethodGet, "/api/compare?serials=SN-A,SN-B", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Drives []struct {
			Serial string `json:"serial"`
			Model  string `json:"model"`
			Health string `json:"health"`
		} `json:"drives"`
		Rows []struct {
			ID      int   `json:"id"`
			Differs bool  `json:"differs"`
			Cells   []any `json:"cells"`
		} `json:"rows"`
		NVMe []any `json:"nvme"`
	}
	decodeBody(t, rec, &resp)

	if len(resp.Drives) != 2 || resp.Drives[0].Serial != "SN-A" || resp.Drives[1].Serial != "SN-B" {
		t.Fatalf("drives = %+v, want the two asked for, in order", resp.Drives)
	}
	if resp.Drives[0].Model != "Samsung SSD 870 EVO" {
		t.Errorf("model = %q, want the stored model", resp.Drives[0].Model)
	}
	if resp.Drives[1].Health != "caution" {
		t.Errorf("health = %q, want the stored verdict", resp.Drives[1].Health)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (attributes 5 and 9)", len(resp.Rows))
	}
	// Every row carries one cell per drive, so a cell's position is its column.
	for _, row := range resp.Rows {
		if len(row.Cells) != 2 {
			t.Errorf("row %d has %d cells, want one per drive", row.ID, len(row.Cells))
		}
	}
	if len(resp.NVMe) != 2 {
		t.Errorf("nvme = %d entries, want one per drive", len(resp.NVMe))
	}
	// The sector row differs in the raw count only, which is the whole point of comparing it.
	if !resp.Rows[0].Differs {
		t.Errorf("row %+v should differ", resp.Rows[0])
	}
}

// A serial nobody has is a 404 naming it, not an empty column: a comparison that silently dropped a
// drive the user asked for would be answering a different question.
func TestCompareEndpointRejectsAnUnknownDrive(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	saveComparisonFixtures(t, st)

	rec := do(t, srv, http.MethodGet, "/api/compare?serials=SN-A,nope", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "nope") {
		t.Errorf("body = %s, want it to name the drive", rec.Body.String())
	}
}

func TestCompareEndpointRejectsOneDrive(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodGet, "/api/compare?serials=SN-A", "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// saveComparisonFixtures stores two drives of one model, each with an attribute blob: attribute 5
// differing in the raw count while both normalised values are 100, and attribute 9 differing in
// both.
func saveComparisonFixtures(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	blob := func(reallocRaw, hours uint64) string {
		payload := map[string]any{"attributes": []model.Attribute{
			{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100, Worst: 100, Threshold: 10, RawValue: reallocRaw},
			{ID: 9, Name: "Power_On_Hours", Value: 98, Worst: 98, RawValue: hours},
		}}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		return string(raw)
	}

	_, err := st.SaveScan(ctx, "manual", now, now, []store.ScannedDevice{
		{Serial: "SN-A", Model: "Samsung SSD 870 EVO", DevicePath: "/dev/sda",
			Health: "good", Score: 95, AttributesJSON: blob(0, 1200)},
		{Serial: "SN-B", Model: "Samsung SSD 870 EVO", DevicePath: "/dev/sdb",
			Health: "caution", Score: 70, AttributesJSON: blob(12, 4300)},
	})
	if err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
}
