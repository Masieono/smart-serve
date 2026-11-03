package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/store"
)

// `FEATURES.md` A12: the score breakdown has to survive the scan that produced it.
//
// It never did. The interpreter computed a per-drive list of reasons and points on every read and
// the app threw it away — so the one question a score invites ("why is it 72?") had an answer that
// existed for a few milliseconds. This is the test that says it now reaches the page.
func TestTheScoreBreakdownSurvivesAScanAndReachesTheDevicePayload(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	saveDeviceWithPenalties(t, st)

	rec := do(t, srv, http.MethodGet, "/api/devices/SN-PEN", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Device struct {
			Score     int `json:"score"`
			Penalties []struct {
				Reason      string  `json:"reason"`
				Points      float64 `json:"points"`
				AttributeID int     `json:"attribute_id"`
			} `json:"penalties"`
		} `json:"device"`
	}
	decodeBody(t, rec, &resp)

	if len(resp.Device.Penalties) != 2 {
		t.Fatalf("penalties = %+v, want the two that were stored", resp.Device.Penalties)
	}
	if resp.Device.Penalties[0].Reason != "reallocated sectors (12)" {
		t.Errorf("reason = %q", resp.Device.Penalties[0].Reason)
	}
	if resp.Device.Penalties[0].Points != 30 {
		t.Errorf("points = %v, want 30", resp.Device.Penalties[0].Points)
	}
	// The attribute id is what lets the table mark the row the verdict read, so it has to survive
	// the round trip as well as the reason does.
	if resp.Device.Penalties[0].AttributeID != 5 {
		t.Errorf("attribute_id = %d, want 5", resp.Device.Penalties[0].AttributeID)
	}
	// A line that is not about one attribute carries none, rather than a zero that would mark
	// attribute 0.
	if resp.Device.Penalties[1].AttributeID != 0 {
		t.Errorf("attribute_id = %d for a line with no attribute, want it absent",
			resp.Device.Penalties[1].AttributeID)
	}
}

// The breakdown is detail-only. A fleet table shows a score, and six reasons per drive is not
// something a list can carry — so the list must not grow a field it has no room for.
func TestTheDeviceListCarriesNoBreakdown(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	saveDeviceWithPenalties(t, st)

	rec := do(t, srv, http.MethodGet, "/api/devices", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "reallocated sectors (12)") {
		t.Error("the device list carries the breakdown; it belongs on the detail endpoint only")
	}
}

// A drive that scored cleanly has no breakdown, and the field is absent rather than an empty list —
// "nothing to explain" and "an explanation of nothing" are the same thing here, and absent is the
// shorter way to say it.
func TestACleanDriveHasNoBreakdown(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := st.SaveScan(ctx, "manual", now, now, []store.ScannedDevice{
		{Serial: "SN-CLEAN", Model: "Samsung SSD 870 EVO", DevicePath: "/dev/sdc", Health: "good", Score: 100},
	}); err != nil {
		t.Fatalf("SaveScan: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/devices/SN-CLEAN", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "penalties") {
		t.Errorf("a clean drive should carry no penalties field: %s", rec.Body.String())
	}
}

// saveDeviceWithPenalties stores one drive whose verdict cost it points, in the shape the scan
// writes: the breakdown as a JSON array, one line naming an attribute and one naming none.
func saveDeviceWithPenalties(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := st.SaveScan(ctx, "manual", now, now, []store.ScannedDevice{{
		Serial: "SN-PEN", Model: "Samsung SSD 870 EVO", DevicePath: "/dev/sdp",
		Health: "bad", Score: 39,
		HealthRationale: "12 reallocated sectors",
		Penalties: `[{"reason":"reallocated sectors (12)","points":30,"attribute_id":5},` +
			`{"reason":"sectors still climbing (7 new)","points":5}]`,
	}})
	if err != nil {
		t.Fatalf("SaveScan: %v", err)
	}
}
