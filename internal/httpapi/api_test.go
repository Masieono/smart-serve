package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/store"
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

// countMissing counts the drives flagged as not present in the latest scan.
func countMissing(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var resp struct {
		Devices []struct {
			IsMissing bool `json:"is_missing"`
		} `json:"devices"`
	}
	decodeBody(t, rec, &resp)
	missing := 0
	for _, d := range resp.Devices {
		if d.IsMissing {
			missing++
		}
	}
	return missing
}

func TestDeviceScanRescansOnlyThatDrive(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	if got := countMissing(t, do(t, srv, http.MethodGet, "/api/devices", "", nil)); got != 0 {
		t.Fatalf("after a full scan %d drives are missing, want 0", got)
	}

	rec := do(t, srv, http.MethodPost, "/api/devices/S1/scan", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("device scan status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Serial       string `json:"serial"`
		Status       string `json:"status"`
		DevicesTotal int    `json:"devices_total"`
	}
	decodeBody(t, rec, &resp)
	if resp.Serial != "S1" || resp.DevicesTotal != 1 {
		t.Errorf("response = %+v, want a single-device scan of S1", resp)
	}

	// The regression this guards: a single-drive save that reused the full-scan path
	// would mark every drive it did not look at as disconnected.
	if got := countMissing(t, do(t, srv, http.MethodGet, "/api/devices", "", nil)); got != 0 {
		t.Errorf("a single-drive scan marked %d other drives disconnected", got)
	}
}

func TestDeviceScanUnknownSerial(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodPost, "/api/devices/nope/scan", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestForgetDeviceRemovesEverythingDerivedFromIt(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	// A note and a snapshot, so the cleanup is observable rather than implied.
	if rec := do(t, srv, http.MethodPut, "/api/devices/S1/notes", `{"name":"boot"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("notes status = %d", rec.Code)
	}

	rec := do(t, srv, http.MethodDelete, "/api/devices/S1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("forget status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if rec := do(t, srv, http.MethodGet, "/api/devices/S1", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("a forgotten device is still readable (status %d)", rec.Code)
	}

	// Snapshots must go with it, or the drive keeps reappearing in history and trends.
	series, err := st.SnapshotSeries(context.Background(), "S1", time.Time{}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("SnapshotSeries: %v", err)
	}
	if len(series) != 0 {
		t.Errorf("snapshots = %d, want 0 after forgetting the device", len(series))
	}

	// A later scan of the same host must not resurrect it: it is not connected.
	if rec := do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("rescan status = %d", rec.Code)
	}
	if got := countMissing(t, do(t, srv, http.MethodGet, "/api/devices", "", nil)); got != 0 {
		t.Errorf("missing = %d, want 0 — a forgotten device should not come back as missing", got)
	}
}

func TestForgetDeviceUnknownSerial(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodDelete, "/api/devices/nope", "", nil)
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

// Raw output used to live only in the scanner's in-memory result. After a restart that
// result is empty while the database is full, so every drive 404'd — which is exactly
// the state a container is in when it comes back up against a persisted volume.
func TestRawOutputSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "restart.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	runner := &fakeRunner{devices: sampleDevices()}
	hub := events.NewHub()

	before := New(st, scan.NewManager(runner, st, hub), hub, Options{})
	do(t, before, http.MethodPost, "/api/scan?wait=1", "", nil)
	if rec := do(t, before, http.MethodGet, "/api/devices/S1/raw", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("raw before restart = %d", rec.Code)
	}

	// A new process: fresh manager, same database, no scan run in it yet.
	after := New(st, scan.NewManager(runner, st, hub), hub, Options{})
	rec := do(t, after, http.MethodGet, "/api/devices/S1/raw", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("raw after restart = %d, want 200 — it has to come from the store", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/dev/sda") {
		t.Errorf("raw body = %s", rec.Body.String())
	}
}

// A single-drive rescan used to replace the in-memory result with a one-device one,
// which broke the raw view for every drive that was not just rescanned.
func TestRawOutputSurvivesASingleDriveRescan(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "rescan.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// Two independent devices: sampleDevices builds fresh values on every call.
	first := sampleDevices()[0]
	second := sampleDevices()[0]
	second.Discovered.Serial = "S2"
	second.Discovered.Path = "/dev/sdb"
	second.Data.Serial = "S2"
	second.Raw = []byte(`{"device":{"name":"/dev/sdb"}}`)

	runner := &fakeRunner{devices: []scan.Device{first, second}}
	hub := events.NewHub()
	srv := New(st, scan.NewManager(runner, st, hub), hub, Options{})

	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)
	do(t, srv, http.MethodPost, "/api/devices/S2/scan", "", nil)

	rec := do(t, srv, http.MethodGet, "/api/devices/S1/raw", "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("raw for the drive that was not rescanned = %d, want 200", rec.Code)
	}
}

// A drive with nothing stored says so usefully, rather than 404ing with no explanation.
func TestRawOutputExplainsItselfWhenEmpty(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	// A device row with no raw output, as one recorded before this was persisted.
	now := time.Now()
	if _, err := st.SaveScan(context.Background(), "manual", now, now,
		[]store.ScannedDevice{{Serial: "NORAW", Health: "good", Score: 100}}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/devices/NORAW/raw", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "rescan") {
		t.Errorf("body = %s, want it to suggest a rescan", rec.Body.String())
	}
}
