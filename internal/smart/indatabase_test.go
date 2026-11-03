// `in_smartctl_database` (`FEATURES.md` A28).
//
// The attribute table used to *infer* "smartctl does not know this model" from seeing
// `Unknown_Attribute` names. This says it — and it is tri-state, because "smartctl did not say" is a
// third answer rather than a missing one, and the note under the table words all three differently.

package smart

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInSmartctlDatabaseIsRead(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "ata-850-evo-logs.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	sd, err := ParseSMART(raw)
	if err != nil {
		t.Fatalf("ParseSMART: %v", err)
	}

	if sd.InDatabase == nil {
		t.Fatal("the fixture says smartctl knows this drive, and the field came back absent")
	}
	if !*sd.InDatabase {
		t.Error("InDatabase = false for a drive smartctl has in its database")
	}
}

// A drive smartctl does not know reports false — which is the fact the attribute table's note was
// previously guessing at.
func TestADriveSmartctlDoesNotKnowReportsFalse(t *testing.T) {
	raw := []byte(`{"json_format_version":[1,0],"in_smartctl_database":false,
		"device":{"name":"/dev/sdz","type":"ata"},
		"model_name":"VENDOR USB DISK","ata_smart_attributes":{"table":[
			{"id":1,"name":"Unknown_Attribute","value":100,"worst":100,"thresh":0,
			 "raw":{"value":0,"string":"0"}}]}}`)
	sd, err := ParseSMART(raw)
	if err != nil {
		t.Fatalf("ParseSMART: %v", err)
	}
	if sd.InDatabase == nil || *sd.InDatabase {
		t.Errorf("InDatabase = %v, want an explicit false", sd.InDatabase)
	}
}

// Absent stays absent. A smartctl that did not say must not be read as "no", because the note under
// the table says something different for each — and claiming smartctl does not know a drive it never
// commented on would be the app inventing a fact about it.
func TestAMissingInSmartctlDatabaseStaysAbsent(t *testing.T) {
	raw := []byte(`{"json_format_version":[1,0],
		"device":{"name":"/dev/sdz","type":"ata"},"model_name":"Something",
		"ata_smart_attributes":{"table":[]}}`)
	sd, err := ParseSMART(raw)
	if err != nil {
		t.Fatalf("ParseSMART: %v", err)
	}
	if sd.InDatabase != nil {
		t.Errorf("InDatabase = %v, want nil when smartctl said nothing", *sd.InDatabase)
	}
}
