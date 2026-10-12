package smart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/masieono/smart-serve/internal/model"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParseSMARTATA(t *testing.T) {
	sd, err := ParseSMART(loadFixture(t, "ata-healthy.json"))
	if err != nil {
		t.Fatalf("ParseSMART: %v", err)
	}
	if sd.Bus != model.BusATA {
		t.Errorf("bus = %q, want ata", sd.Bus)
	}
	if sd.Model != "Samsung SSD 870 EVO 1TB" {
		t.Errorf("model = %q", sd.Model)
	}
	if sd.Serial != "S5GXNX0R512345J" {
		t.Errorf("serial = %q", sd.Serial)
	}
	if sd.Firmware != "SVT02B6Q" {
		t.Errorf("firmware = %q", sd.Firmware)
	}
	if sd.CapacityBytes != 1000204886016 {
		t.Errorf("capacity = %d", sd.CapacityBytes)
	}
	if sd.Passed == nil || !*sd.Passed {
		t.Errorf("passed = %v, want true", sd.Passed)
	}
	if sd.TemperatureC == nil || *sd.TemperatureC != 34 {
		t.Errorf("temperature = %v, want 34", sd.TemperatureC)
	}
	if sd.PowerOnHours == nil || *sd.PowerOnHours != 1234 {
		t.Errorf("power-on hours = %v, want 1234", sd.PowerOnHours)
	}
	if a := sd.AttributeByID(5); a == nil || a.Threshold != 10 {
		t.Errorf("attribute 5 = %+v", a)
	}
	if sd.WWN == "" {
		t.Errorf("wwn is empty")
	}
}

func TestParseSMARTNVMe(t *testing.T) {
	sd, err := ParseSMART(loadFixture(t, "nvme-healthy.json"))
	if err != nil {
		t.Fatalf("ParseSMART: %v", err)
	}
	if sd.Bus != model.BusNVMe {
		t.Errorf("bus = %q, want nvme", sd.Bus)
	}
	if sd.NVMe == nil {
		t.Fatal("NVMe health log missing")
	}
	if sd.NVMe.PercentageUsed != 4 {
		t.Errorf("percentage_used = %d, want 4", sd.NVMe.PercentageUsed)
	}
	if sd.NVMe.AvailableSpare != 100 {
		t.Errorf("available_spare = %d, want 100", sd.NVMe.AvailableSpare)
	}
	if sd.TemperatureC == nil || *sd.TemperatureC != 38 {
		t.Errorf("temperature = %v, want 38 (from top-level)", sd.TemperatureC)
	}
}

// Some NVMe drives report hours and cycles only inside the health log, so the
// parser must fall back to it (regression: the detail page showed "—").
func TestParseSMARTNVMeFallsBackToHealthLog(t *testing.T) {
	sd, err := ParseSMART(loadFixture(t, "nvme-log-only.json"))
	if err != nil {
		t.Fatalf("ParseSMART: %v", err)
	}
	if sd.PowerOnHours == nil || *sd.PowerOnHours != 31603 {
		t.Errorf("power-on hours = %v, want 31603 from the NVMe health log", sd.PowerOnHours)
	}
	if sd.PowerCycles == nil || *sd.PowerCycles != 289 {
		t.Errorf("power cycles = %v, want 289 from the NVMe health log", sd.PowerCycles)
	}
	if sd.NVMe == nil {
		t.Fatal("NVMe health log missing")
	}
	if sd.NVMe.DataUnitsWritten != 2345678 || sd.NVMe.DataUnitsRead != 12345678 {
		t.Errorf("data units = %d/%d", sd.NVMe.DataUnitsRead, sd.NVMe.DataUnitsWritten)
	}
	if sd.NVMe.UnsafeShutdowns != 76 || sd.NVMe.ErrorLogEntries != 1022 {
		t.Errorf("extra NVMe fields not parsed: %+v", sd.NVMe)
	}
}

func TestParseSMARTInvalid(t *testing.T) {
	if _, err := ParseSMART([]byte("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}
