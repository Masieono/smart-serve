package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// fakeRunner stands in for the real scanner so API tests never touch hardware.
type fakeRunner struct {
	devices []scan.Device
	err     error
	calls   int
}

func (f *fakeRunner) Run(context.Context) ([]scan.Device, error) {
	f.calls++
	return f.devices, f.err
}

func sampleDevices() []scan.Device {
	passed := true
	temp := 34
	hours := int64(1234)
	size := uint64(1000204886016)

	sd := &model.SmartData{
		Model: "Samsung SSD 870 EVO", Serial: "S1", Firmware: "SVT02B6Q",
		Bus: model.BusATA, Passed: &passed, TemperatureC: &temp,
		PowerOnHours: &hours, CapacityBytes: size, RotationRate: 0,
		Attributes: []model.Attribute{
			{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100, Worst: 100, Threshold: 10, RawValue: 0},
			{ID: 197, Name: "Current_Pending_Sector", Value: 100, Worst: 100, RawValue: 0},
		},
	}
	return []scan.Device{{
		Discovered: smart.Discovered{
			Path: "/dev/sda", Model: "Samsung SSD 870 EVO", Serial: "S1",
			Transport: "sata", SizeBytes: size,
			Mountpoints: []string{"/"}, Fstype: "ext4",
		},
		Data:   sd,
		Health: interp.Evaluate(sd, interp.DefaultThresholds()),
		Raw:    []byte(`{"device":{"name":"/dev/sda"}}`),
	}}
}

func newTestServer(t *testing.T, opts Options) (*Server, *store.Store, *fakeRunner, *events.Hub) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	runner := &fakeRunner{devices: sampleDevices()}
	hub := events.NewHub()
	manager := scan.NewManager(runner, st, hub)
	return New(st, manager, hub, opts), st, runner, hub
}

func do(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}
