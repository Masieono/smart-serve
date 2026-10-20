package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/store"
)

// C6 is **off by default**, and that is the whole of the idle-cost argument: the handler does no work
// of its own, but enabling it invites someone else's scraper to poll this app on a schedule, and that
// is the operator's decision rather than a default (`ADR-023`).
func TestMetricsAreOffUntilEnabled(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	// 404 rather than 403: the honest answer to "is this feature on?" is "it is not here", and a 403
	// would advertise an endpoint the operator did not enable.
	if rec := do(t, srv, http.MethodGet, "/metrics", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 while disabled", rec.Code)
	}

	if err := st.SetSettings(context.Background(), map[string]string{"prometheus_enabled": "true"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/metrics", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content type = %q, want the exposition format", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"# HELP smartserve_device_info",
		"# TYPE smartserve_device_info gauge",
		`smartserve_device_info{health="good",model="Samsung SSD 870 EVO",serial="S1",transport="sata"} 1`,
		"smartserve_device_score{serial=\"S1\"} ",
		"smartserve_device_temperature_celsius{serial=\"S1\"} 34",
		"smartserve_device_power_on_hours{serial=\"S1\"} 1234",
		"smartserve_devices_total 1",
		`smartserve_devices{health="good"} 1`,
		"smartserve_build_info",
		"smartserve_scan_last_success_timestamp_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape is missing %q:\n%s", want, body)
		}
	}
	// A label with no value is dropped rather than rendered as an empty one, because the format
	// treats the two as equivalent and `name=""` on every drive is noise in a diff.
	if strings.Contains(body, `name=""`) {
		t.Errorf("an empty label was rendered:\n%s", body)
	}
}

// A reading the drive did not report drops the series rather than exporting a zero — the same rule as
// the em dash on screen, and here it is also what Prometheus expects: an absent series means "no
// measurement", while a 0 is a claim that the drive reported zero.
func TestMetricsOmitReadingsTheDriveDidNotReport(t *testing.T) {
	var b strings.Builder
	writeMetrics(&b, "test", []store.DeviceRecord{{
		Serial: "S1", Health: "unknown", Score: -1, // never scored
	}}, nil, nil, nil)
	body := b.String()

	for _, unwanted := range []string{
		"smartserve_device_score",
		"smartserve_device_temperature_celsius",
		"smartserve_device_power_on_hours",
		"smartserve_device_wear_percent",
		"smartserve_device_new_sectors",
		// No scan has completed, so there is no timestamp to report.
		"smartserve_scan_last_success_timestamp_seconds",
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a drive that reported nothing exported %q:\n%s", unwanted, body)
		}
	}
	// The identity and the count are still there: the drive exists, and that is not a measurement.
	if !strings.Contains(body, "smartserve_devices_total 1") {
		t.Errorf("the inventory is missing:\n%s", body)
	}
	if !strings.Contains(body, `smartserve_devices{health="unknown"} 1`) {
		t.Errorf("the verdict count is missing:\n%s", body)
	}
}

// The exposition format reserves three characters inside a label value, and a drive name is free text
// a user typed — a quote or a backslash in it must not corrupt the scrape.
func TestMetricsEscapeLabelValues(t *testing.T) {
	var b strings.Builder
	writeMetrics(&b, "1.0.0", []store.DeviceRecord{{
		Serial: `S"1`, Name: `parity\backup`, Model: "two\nlines", Health: "good",
	}}, nil, nil, nil)
	body := b.String()

	if !strings.Contains(body, `name="parity\\backup"`) {
		t.Errorf("backslash not escaped:\n%s", body)
	}
	if !strings.Contains(body, `serial="S\"1"`) {
		t.Errorf("quote not escaped:\n%s", body)
	}
	if !strings.Contains(body, `model="two\nlines"`) {
		t.Errorf("newline not escaped:\n%s", body)
	}
}

// A scrape is diffed by whoever reads it, so the sample order must be stable: an exporter whose
// output wanders makes every scrape look like a change.
func TestMetricsOutputIsStable(t *testing.T) {
	devices := []store.DeviceRecord{
		{Serial: "S2", Health: "caution", Score: 60},
		{Serial: "S1", Health: "good", Score: 90},
		{Serial: "S3", Health: "bad", Score: 10},
	}
	first := scrape(devices)
	for i := 0; i < 5; i++ {
		if got := scrape(devices); got != first {
			t.Fatalf("scrape %d differs:\n%s\n---\n%s", i, first, got)
		}
	}
	// And sorted, not merely stable: a map iteration can be stable within one call and shuffle between
	// them, which is the failure this guards against. Asserted on the score family, where the serial
	// is the only label and so the order is unambiguous.
	scores := first[strings.Index(first, "# TYPE smartserve_device_score"):]
	scores = scores[:strings.Index(scores, "# HELP smartserve_devices_total")]
	if strings.Index(scores, `serial="S1"`) > strings.Index(scores, `serial="S2"`) {
		t.Errorf("samples are not in label order:\n%s", scores)
	}
}

func scrape(devices []store.DeviceRecord) string {
	var b strings.Builder
	writeMetrics(&b, "test", devices, nil, nil, nil)
	return b.String()
}
