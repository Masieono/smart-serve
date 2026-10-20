package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// scanFailingDrive makes S1 come back bad, so there is a condition to acknowledge.
func scanFailingDrive(t *testing.T, srv *Server, runner *fakeRunner) {
	t.Helper()
	devices := sampleDevices()
	// Both the raw counter and the verdict, so the drive is bad however the manager derives it.
	devices[0].Data.Attributes[0].RawValue = 40
	devices[0].Health = interp.Result{
		Verdict: model.Bad, Rationale: "40 reallocated sectors", Score: 20,
	}
	runner.devices = devices
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)
}

// B7: acknowledging a condition silences its alerts and changes no finding.
func TestAcknowledgeADevice(t *testing.T) {
	srv, st, runner, _ := newTestServer(t, Options{})
	scanFailingDrive(t, srv, runner)

	rec := do(t, srv, http.MethodPut, "/api/devices/S1/ack", `{"note":"RMA raised"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Ack *ackJSON `json:"ack"`
	}
	decodeBody(t, rec, &body)
	if body.Ack == nil || body.Ack.Health != "bad" || body.Ack.Note != "RMA raised" {
		t.Fatalf("ack = %+v", body.Ack)
	}
	// The verdict is untouched: the ack hides the repetition, not the finding.
	dev, err := st.GetDevice(context.Background(), "S1")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if dev.Health != "bad" {
		t.Errorf("health = %q, want it left as bad", dev.Health)
	}

	// The ack is on the device payload, so the UI can say so wherever the verdict is shown.
	rec = do(t, srv, http.MethodGet, "/api/devices/S1", "", nil)
	var detail struct {
		Device deviceJSON `json:"device"`
	}
	decodeBody(t, rec, &detail)
	if detail.Device.Ack == nil || detail.Device.Ack.Note != "RMA raised" {
		t.Errorf("device payload carries no acknowledgement: %+v", detail.Device.Ack)
	}

	// And it comes off again.
	rec = do(t, srv, http.MethodDelete, "/api/devices/S1/ack", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("unack status = %d", rec.Code)
	}
	decodeBody(t, rec, &body)
	if body.Ack != nil {
		t.Errorf("ack = %+v, want it cleared", body.Ack)
	}
}

// A drive with nothing wrong has nothing to acknowledge. Refused rather than allowed, because an ack
// on a healthy drive would sit there waiting to silence the first problem it develops.
func TestAcknowledgeRefusesAHealthyDrive(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	rec := do(t, srv, http.MethodPut, "/api/devices/S1/ack", `{"note":"why not"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestAcknowledgeAnUnknownDevice(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	if rec := do(t, srv, http.MethodPut, "/api/devices/NOPE/ack", `{}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// An acknowledgement is user-assigned metadata about a drive, so editing the drive's name must not
// silently drop it — `device_notes` is one row and the ack lives in it.
func TestEditingNotesKeepsTheAcknowledgement(t *testing.T) {
	srv, st, runner, _ := newTestServer(t, Options{})
	scanFailingDrive(t, srv, runner)
	if rec := do(t, srv, http.MethodPut, "/api/devices/S1/ack", `{"note":"known"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("ack status = %d", rec.Code)
	}

	if err := st.SetNotes(context.Background(), store.Notes{Serial: "S1", Name: "parity"}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	ack, err := st.AckFor(context.Background(), "S1")
	if err != nil {
		t.Fatalf("AckFor: %v", err)
	}
	if ack == nil || ack.Note != "known" {
		t.Errorf("ack = %+v, want it kept through a notes edit", ack)
	}
}
