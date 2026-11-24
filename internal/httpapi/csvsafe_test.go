package httpapi

import (
	"encoding/csv"
	"strings"
	"testing"
)

// The characters that make a spreadsheet cell executable, and the ones it ignores.
func TestCSVSafeNeutralisesFormulas(t *testing.T) {
	for _, in := range []string{
		"=cmd|' /C calc'!A0",
		"+1+1",
		"-2+3",
		"@SUM(A1)",
		"\t=1+1",
		"\r=1+1",
	} {
		got := csvSafe(in)
		if !strings.HasPrefix(got, "'") {
			t.Errorf("csvSafe(%q) = %q, want a leading apostrophe", in, got)
		}
	}

	// Ordinary values are untouched, including ones that merely contain a sign.
	for _, in := range []string{"", "Samsung SSD 870", "WDC WD20EURX", "12 - 3", "notes=ok"} {
		if got := csvSafe(in); got != in {
			t.Errorf("csvSafe(%q) = %q, want it unchanged", in, got)
		}
	}
}

// A drive's model string is whatever its firmware says, and these exports exist to be opened in a
// spreadsheet. The value has to arrive quoted as text, not executed.
func TestFleetExportQuotesAModelThatLooksLikeAFormula(t *testing.T) {
	rows := []fleetRow{{
		Serial: "S1",
		Model:  "=cmd|' /C calc'!A0",
	}}
	out := renderFleetCSV(rows)

	reader := csv.NewReader(strings.NewReader(out))
	// The export opens with a preamble and a blank line on purpose; the fields are what matter.
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("the export is not valid CSV: %v", err)
	}
	found := false
	for _, rec := range records {
		for _, field := range rec {
			if strings.Contains(field, "calc") {
				found = true
				if !strings.HasPrefix(field, "'") {
					t.Errorf("field = %q, want it prefixed so a spreadsheet treats it as text", field)
				}
			}
		}
	}
	if !found {
		t.Fatal("the model never reached the export")
	}
}
