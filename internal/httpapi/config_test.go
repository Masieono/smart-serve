package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodPut, "/api/settings",
		`{"temperature_threshold_c":"60","theme":"dark"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("put settings status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, http.MethodGet, "/api/settings", "", nil)
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	decodeBody(t, rec, &resp)
	if resp.Settings["temperature_threshold_c"] != "60" || resp.Settings["theme"] != "dark" {
		t.Errorf("settings = %v", resp.Settings)
	}
}

func schedulePath(id int64) string {
	return "/api/schedules/" + strconv.FormatInt(id, 10)
}

func TestScheduleCRUD(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodPost, "/api/schedules",
		`{"kind":"interval","every_seconds":21600,"label":"every 6h","enabled":true}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Schedule struct {
			ID           int64  `json:"id"`
			Kind         string `json:"kind"`
			EverySeconds *int64 `json:"every_seconds"`
			Label        string `json:"label"`
			Enabled      bool   `json:"enabled"`
		} `json:"schedule"`
	}
	decodeBody(t, rec, &created)
	if created.Schedule.ID == 0 || created.Schedule.Kind != "interval" || !created.Schedule.Enabled {
		t.Fatalf("created = %+v", created.Schedule)
	}

	// An interval schedule without every_seconds is rejected.
	rec = do(t, srv, http.MethodPost, "/api/schedules", `{"kind":"interval"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid schedule status = %d, want 400", rec.Code)
	}

	// Switch it to a cron schedule and disable it.
	rec = do(t, srv, http.MethodPut, schedulePath(created.Schedule.ID),
		`{"kind":"cron","cron":"0 3 * * *","label":"nightly","enabled":false}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, http.MethodGet, "/api/schedules", "", nil)
	var list struct {
		Schedules []struct {
			Kind    string `json:"kind"`
			Cron    string `json:"cron"`
			Enabled bool   `json:"enabled"`
		} `json:"schedules"`
	}
	decodeBody(t, rec, &list)
	if len(list.Schedules) != 1 || list.Schedules[0].Kind != "cron" ||
		list.Schedules[0].Cron != "0 3 * * *" || list.Schedules[0].Enabled {
		t.Errorf("schedules = %+v", list.Schedules)
	}

	rec = do(t, srv, http.MethodDelete, schedulePath(created.Schedule.ID), "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("delete status = %d", rec.Code)
	}
	rec = do(t, srv, http.MethodGet, "/api/schedules", "", nil)
	decodeBody(t, rec, &list)
	if len(list.Schedules) != 0 {
		t.Errorf("schedules after delete = %+v", list.Schedules)
	}
}

func TestChannelCRUD(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	rec := do(t, srv, http.MethodPost, "/api/channels",
		`{"type":"ntfy","label":"alerts","config":{"topic":"x"},"events":["health_change"]}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Channel struct {
			ID      int64           `json:"id"`
			Type    string          `json:"type"`
			Label   string          `json:"label"`
			Enabled bool            `json:"enabled"`
			Config  json.RawMessage `json:"config"`
		} `json:"channel"`
	}
	decodeBody(t, rec, &created)
	if created.Channel.ID == 0 || created.Channel.Type != "ntfy" || !created.Channel.Enabled {
		t.Fatalf("created = %+v", created.Channel)
	}
	if string(created.Channel.Config) != `{"topic":"x"}` {
		t.Errorf("config = %s", created.Channel.Config)
	}

	// Delivery is not implemented yet, so /test is an explicit 501.
	rec = do(t, srv, http.MethodPost,
		"/api/channels/"+strconv.FormatInt(created.Channel.ID, 10)+"/test", "", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("test channel status = %d, want 501", rec.Code)
	}

	rec = do(t, srv, http.MethodDelete,
		"/api/channels/"+strconv.FormatInt(created.Channel.ID, 10), "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("delete status = %d", rec.Code)
	}
	rec = do(t, srv, http.MethodGet, "/api/channels", "", nil)
	var list struct {
		Channels []json.RawMessage `json:"channels"`
	}
	decodeBody(t, rec, &list)
	if len(list.Channels) != 0 {
		t.Errorf("channels after delete = %d", len(list.Channels))
	}
}
