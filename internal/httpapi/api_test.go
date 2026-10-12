package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok\n" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestScanThenListDevices(t *testing.T) {
	srv, _, runner, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("scan status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var scanResp struct {
		Status       string `json:"status"`
		DevicesTotal int    `json:"devices_total"`
		ScanID       int64  `json:"scan_id"`
	}
	decodeBody(t, rec, &scanResp)
	if scanResp.Status != "completed" || scanResp.DevicesTotal != 1 || scanResp.ScanID == 0 {
		t.Fatalf("scan response = %+v", scanResp)
	}
	if runner.calls != 1 {
		t.Errorf("runner calls = %d, want 1", runner.calls)
	}

	rec = do(t, srv, http.MethodGet, "/api/devices", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var listResp struct {
		Devices []struct {
			Serial       string   `json:"serial"`
			Health       string   `json:"health"`
			Score        int      `json:"score"`
			Model        string   `json:"model"`
			Mountpoints  []string `json:"mountpoints"`
			Fstype       string   `json:"fstype"`
			TemperatureC *int     `json:"temperature_c"`
			LastScan     *string  `json:"last_scan"`
		} `json:"devices"`
		LastScanTS *string `json:"last_scan_ts"`
	}
	decodeBody(t, rec, &listResp)
	if len(listResp.Devices) != 1 {
		t.Fatalf("devices = %+v", listResp.Devices)
	}
	d := listResp.Devices[0]
	if d.Serial != "S1" || d.Health != "good" || d.Score != 100 {
		t.Errorf("device = %+v", d)
	}
	if len(d.Mountpoints) != 1 || d.Mountpoints[0] != "/" || d.Fstype != "ext4" {
		t.Errorf("mountpoints = %v, fstype = %q", d.Mountpoints, d.Fstype)
	}
	if d.TemperatureC == nil || *d.TemperatureC != 34 {
		t.Errorf("temperature = %v", d.TemperatureC)
	}
	if listResp.LastScanTS == nil {
		t.Error("last_scan_ts should be set after a scan")
	}
}

func TestGetDeviceIncludesSnapshot(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodGet, "/api/devices/S1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Device struct {
			Serial string `json:"serial"`
		} `json:"device"`
		Snapshot *struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"snapshot"`
	}
	decodeBody(t, rec, &resp)
	if resp.Device.Serial != "S1" {
		t.Errorf("device = %+v", resp.Device)
	}
	if resp.Snapshot == nil {
		t.Fatal("snapshot missing")
	}
	if _, ok := resp.Snapshot.Attributes["attributes"]; !ok {
		t.Errorf("snapshot attributes = %+v", resp.Snapshot.Attributes)
	}

	// Unknown device is a clean 404.
	rec = do(t, srv, http.MethodGet, "/api/devices/nope", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown device status = %d, want 404", rec.Code)
	}
}

func TestSetNotes(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodPut, "/api/devices/S1/notes",
		`{"name":"boot","notes":"bought 2024","tags":"critical","hidden":false}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("notes status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, http.MethodGet, "/api/devices", "", nil)
	var listResp struct {
		Devices []struct {
			Serial string `json:"serial"`
			Name   string `json:"name"`
			Notes  string `json:"notes"`
			Tags   string `json:"tags"`
		} `json:"devices"`
	}
	decodeBody(t, rec, &listResp)
	if listResp.Devices[0].Name != "boot" || listResp.Devices[0].Notes != "bought 2024" {
		t.Errorf("device = %+v", listResp.Devices[0])
	}

	// Full-replace semantics: omitting a field clears it.
	rec = do(t, srv, http.MethodPut, "/api/devices/S1/notes", `{"name":"os"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("notes update status = %d", rec.Code)
	}
	rec = do(t, srv, http.MethodGet, "/api/devices/S1", "", nil)
	var resp struct {
		Device struct {
			Name  string `json:"name"`
			Notes string `json:"notes"`
		} `json:"device"`
	}
	decodeBody(t, rec, &resp)
	if resp.Device.Name != "os" || resp.Device.Notes != "" {
		t.Errorf("after replace: %+v", resp.Device)
	}
}

func TestReportExport(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodGet, "/api/devices/S1/report", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("report status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"smart-serve report", "Samsung SSD 870 EVO", "Health:", "GOOD"} {
		if !strings.Contains(body, want) {
			t.Errorf("report missing %q:\n%s", want, body)
		}
	}

	rec = do(t, srv, http.MethodGet, "/api/devices/S1/report?format=json", "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("json report status = %d", rec.Code)
	}
}

func TestRawOutput(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodGet, "/api/devices/S1/raw", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("raw status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/dev/sda") {
		t.Errorf("raw body = %s", rec.Body.String())
	}
}
