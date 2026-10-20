package httpapi

import (
	"net/http"
	"strings"

	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/store"
)

// ackJSON is a device's acknowledgement (`FEATURES.md` B7).
type ackJSON struct {
	// Health is the verdict that was acknowledged. Sent because the UI has to say *what* was
	// acknowledged: "acknowledged" on its own does not tell the reader whether the note still
	// applies, or whether the drive has since moved past it.
	Health string `json:"health"`
	Note   string `json:"note"`
	At     string `json:"at"`
}

func toAckJSON(a *store.Ack) *ackJSON {
	if a == nil {
		return nil
	}
	return &ackJSON{Health: a.Health, Note: a.Note, At: rfc3339(a.At)}
}

// handleAckDevice records that the user knows about a drive's condition (`FEATURES.md` B7).
//
// It silences the alerts that describe the *condition* and leaves the verdict alone: the drive still
// reads as caution on the dashboard, because it is still caution. What the ack removes is the
// repetition, not the finding.
//
// The verdict comes from the stored device rather than from the request, because the ack has to name
// the condition the **server** last assessed. A stale client could otherwise acknowledge a verdict
// the drive no longer has, and silence a real change.
func (s *Server) handleAckDevice(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	rec, err := s.store.GetDevice(r.Context(), serial)
	if err != nil {
		s.fail(w, "ack device", err)
		return
	}
	if rec == nil {
		writeError(w, http.StatusNotFound, "unknown device")
		return
	}
	// A drive with nothing wrong has nothing to acknowledge. Refused rather than allowed, because an
	// ack on a healthy drive would sit there waiting to silence the first problem it develops — which
	// is the opposite of what the user meant.
	if model.Verdict(rec.Health) == model.Good {
		writeError(w, http.StatusBadRequest,
			"nothing to acknowledge: this drive is reporting nothing wrong")
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.store.SetAck(r.Context(), serial, rec.Health, strings.TrimSpace(body.Note)); err != nil {
		s.fail(w, "ack device", err)
		return
	}
	s.writeAck(w, r, serial)
}

// handleUnackDevice removes an acknowledgement, so the drive alerts again on the next scan.
func (s *Server) handleUnackDevice(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	if err := s.store.ClearAck(r.Context(), serial); err != nil {
		s.fail(w, "unack device", err)
		return
	}
	s.writeAck(w, r, serial)
}

// writeAck responds with the drive's acknowledgement as it now stands, so the UI never has to guess
// what the server recorded.
func (s *Server) writeAck(w http.ResponseWriter, r *http.Request, serial string) {
	ack, err := s.store.AckFor(r.Context(), serial)
	if err != nil {
		s.fail(w, "read acknowledgement", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"serial": serial, "ack": toAckJSON(ack)})
}
