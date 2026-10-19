package httpapi

import (
	"net/http"

	"github.com/masieono/smart-serve/internal/model"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// selfTestJSON is what the self-test endpoints return.
type selfTestJSON struct {
	// Logs is the same shape the device detail returns, so the UI renders it with the function
	// it already has rather than a second one that can drift from it.
	Logs *model.LogSummary `json:"logs,omitempty"`
	// Running is the test in progress, present only while one is. Computed on the server rather
	// than derived in the UI so the rule for recognising one lives in a single place.
	Running *model.SelfTestEntry `json:"running,omitempty"`
}

// handleGetSelfTest reports a drive's self-test state, read live from the drive.
//
// Deliberately separate from the device detail, which is served from the document the last scan
// stored. Progress needs a fresh reading, and this is the only place the app reads a device
// outside a scan — it happens while someone has the self-test panel open, and the UI stops
// asking once the test ends (`FEATURES.md` §E).
func (s *Server) handleGetSelfTest(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.selfTestTarget(w, r)
	if !ok {
		return
	}
	res, err := smart.ReadSelfTestLog(r.Context(), s.commandTimeout(r.Context()),
		rec.DevicePath, rec.TransportOverride)
	if err != nil {
		s.fail(w, "read self-test log", err)
		return
	}
	writeJSON(w, http.StatusOK, selfTestResponse(smart.ParseLogs(res.Stdout)))
}

// handleStartSelfTest asks a drive to run a self-test.
//
// The one request in this API that changes a drive, which is why it is an explicit POST naming
// the test rather than anything the app decides on its own (`FEATURES.md` A19, `ADR-017`).
func (s *Server) handleStartSelfTest(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.selfTestTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		Kind string `json:"kind"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	kind := smart.SelfTestKind(body.Kind)
	if !kind.Valid() {
		writeError(w, http.StatusBadRequest, `kind must be "short" or "long"`)
		return
	}

	res, err := smart.StartSelfTest(r.Context(), s.commandTimeout(r.Context()),
		rec.DevicePath, rec.TransportOverride, kind)
	if err != nil {
		// 502: the request was well-formed, the drive would not take it. smartctl's own words
		// are the most useful thing to hand back, because "the drive refused" has several
		// causes and only the drive knows which.
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "started",
		"type":   string(kind),
		// The drive's acknowledgement, passed through rather than paraphrased.
		"note": smart.Messages(res.Stdout, res.Stderr),
	})
}

// handleAbortSelfTest stops the test a drive is running.
//
// A long test on a large drive runs for hours, so someone who started one by mistake needs a way
// out that is not "wait". That makes the abort a first-class action rather than a footnote.
func (s *Server) handleAbortSelfTest(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.selfTestTarget(w, r)
	if !ok {
		return
	}
	res, err := smart.AbortSelfTest(r.Context(), s.commandTimeout(r.Context()),
		rec.DevicePath, rec.TransportOverride)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "aborted",
		"note":   smart.Messages(res.Stdout, res.Stderr),
	})
}

// selfTestTarget resolves the drive a self-test request is about, writing the error itself when
// it cannot. A drive with no recorded path cannot be addressed at all, which is a different
// answer from a drive that is merely not connected, and saying so beats a confusing EPERM.
func (s *Server) selfTestTarget(w http.ResponseWriter, r *http.Request) (store.DeviceRecord, bool) {
	rec, err := s.store.GetDevice(r.Context(), r.PathValue("serial"))
	if err != nil {
		s.fail(w, "get device", err)
		return store.DeviceRecord{}, false
	}
	if rec == nil {
		writeError(w, http.StatusNotFound, "unknown device")
		return store.DeviceRecord{}, false
	}
	if rec.DevicePath == "" {
		writeError(w, http.StatusConflict,
			"no device path is recorded for this drive, so it cannot be addressed. Rescan it first.")
		return store.DeviceRecord{}, false
	}
	return *rec, true
}

func selfTestResponse(summary model.LogSummary) selfTestJSON {
	out := selfTestJSON{}
	if !summary.IsEmpty() {
		out.Logs = &summary
	}
	out.Running = summary.Running()
	return out
}
