package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// `A3`: the one setting with structure is refused at the point it is typed.
//
// The failure it would otherwise hide is the quietest in the app — valid JSON, a successful save, a
// row in the editor, and a number that nothing ever reads. Every other setting is a single value the
// scan parses and falls back from, which is the right shape for a scalar and the wrong one here.
func TestPerModelThresholdsAreValidatedOnSave(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})

	bad := []string{
		`{"model_thresholds":"not json"}`,
		`{"model_thresholds":"[]"}`,
		`{"model_thresholds":"{\"870 EVO\":{\"reallocated_bad\":\"1\"}}"}`,
	}
	for _, body := range bad {
		rec := do(t, srv, http.MethodPut, "/api/settings", body, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d for %s, want 400 (body %s)", rec.Code, body, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), "Per-model thresholds") {
			t.Errorf("body = %s, want it to name the setting", rec.Body.String())
		}
	}

	// A rejected save stores nothing, so a typo cannot leave a half-applied setting behind.
	settings, err := st.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if _, ok := settings["model_thresholds"]; ok {
		t.Errorf("a rejected value was stored anyway: %q", settings["model_thresholds"])
	}

	// And a good one is stored and read back.
	good := `{"model_thresholds":"{\"870 EVO\":{\"reallocated_bad_threshold\":\"1\"}}"}`
	rec := do(t, srv, http.MethodPut, "/api/settings", good, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d for a valid value, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	decodeBody(t, rec, &resp)
	if resp.Settings["model_thresholds"] != `{"870 EVO":{"reallocated_bad_threshold":"1"}}` {
		t.Errorf("stored = %q, want the value sent", resp.Settings["model_thresholds"])
	}
}

// A settings save with no structured value in it is unaffected: the validation is additive, so every
// other setting still goes straight through.
func TestPlainSettingsStillSave(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodPut, "/api/settings", `{"temperature_threshold_c":"60"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	decodeBody(t, rec, &resp)
	if resp.Settings["temperature_threshold_c"] != "60" {
		t.Errorf("temperature_threshold_c = %q, want 60", resp.Settings["temperature_threshold_c"])
	}
}
