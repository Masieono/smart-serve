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
			Serial   string `json:"serial"`
			Firmware string `json:"firmware"`
		} `json:"device"`
		Snapshot *struct {
			Attributes map[string]any `json:"attributes"`
			PowerOnHrs *int64         `json:"power_on_hours"`
		} `json:"snapshot"`
		Metrics *struct {
			PowerOnHours     *int64  `json:"power_on_hours"`
			PowerCycles      *int64  `json:"power_cycles"`
			DataReadBytes    *uint64 `json:"data_read_bytes"`
			DataWrittenBytes *uint64 `json:"data_written_bytes"`
		} `json:"metrics"`
	}
	decodeBody(t, rec, &resp)
	if resp.Device.Serial != "S1" {
		t.Errorf("device = %+v", resp.Device)
	}
	if resp.Device.Firmware != "SVT02B6Q" {
		t.Errorf("firmware = %q, want the value persisted from S.M.A.R.T.", resp.Device.Firmware)
	}
	if resp.Snapshot == nil {
		t.Fatal("snapshot missing")
	}
	if _, ok := resp.Snapshot.Attributes["attributes"]; !ok {
		t.Errorf("snapshot attributes = %+v", resp.Snapshot.Attributes)
	}

	// The detail page reads throughput and power figures from metrics, which are
	// derived server-side so the UI need not understand ATA/NVMe layouts.
	if resp.Metrics == nil {
		t.Fatal("metrics missing")
	}
	if resp.Metrics.PowerOnHours == nil || *resp.Metrics.PowerOnHours != 1234 {
		t.Errorf("power-on hours = %v, want 1234", resp.Metrics.PowerOnHours)
	}
	if resp.Metrics.PowerCycles == nil || *resp.Metrics.PowerCycles != 42 {
		t.Errorf("power cycles = %v, want 42", resp.Metrics.PowerCycles)
	}
	if resp.Metrics.DataWrittenBytes == nil || *resp.Metrics.DataWrittenBytes != 1000000*512 {
		t.Errorf("data written = %v, want %d", resp.Metrics.DataWrittenBytes, 1000000*512)
	}
	if resp.Metrics.DataReadBytes == nil || *resp.Metrics.DataReadBytes != 2000000*512 {
		t.Errorf("data read = %v, want %d", resp.Metrics.DataReadBytes, 2000000*512)
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

func TestReportExportFormats(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	// Every format the API advertises must actually render, and must be offered as a
	// download rather than displayed — an export you have to copy out of a browser
	// tab is not an export.
	cases := []struct {
		format      string
		contentType string
		want        []string
	}{
		{"txt", "text/plain", []string{"smart-serve report", "Samsung SSD 870 EVO", "GOOD"}},
		{"html", "text/html", []string{"<!doctype html>", "Samsung SSD 870 EVO", "Reallocated_Sector_Ct"}},
		{"csv", "text/csv", []string{"field,value", "attr 5 Reallocated_Sector_Ct"}},
	}
	for _, c := range cases {
		rec := do(t, srv, http.MethodGet, "/api/devices/S1/report?format="+c.format, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", c.format, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, c.contentType) {
			t.Errorf("%s: content type = %q, want %q", c.format, ct, c.contentType)
		}
		if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "."+c.format) {
			t.Errorf("%s: disposition = %q, want an attachment filename", c.format, cd)
		}
		for _, want := range c.want {
			if !strings.Contains(rec.Body.String(), want) {
				t.Errorf("%s: body missing %q", c.format, want)
			}
		}
	}

	// JSON is the machine format and stays inline.
	rec := do(t, srv, http.MethodGet, "/api/devices/S1/report?format=json", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"device"`) {
		t.Errorf("json: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, http.MethodGet, "/api/devices/S1/report?format=pdf", "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown format status = %d, want 400", rec.Code)
	}
}

// An export must be self-contained: it has to stay readable long after the app has
// been uninstalled, so nothing may point back at the server.
func TestHTMLReportIsSelfContained(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodGet, "/api/devices/S1/report?format=html", "", nil)
	body := rec.Body.String()
	for _, forbidden := range []string{"http://", "https://", "<script", "src="} {
		if strings.Contains(body, forbidden) {
			t.Errorf("html report references %q; it must stand alone", forbidden)
		}
	}
	if !strings.Contains(body, "<style>") {
		t.Error("html report should carry its own styles")
	}
}

func TestReportExportUnknownDevice(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodGet, "/api/devices/nope/report?format=html", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
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
