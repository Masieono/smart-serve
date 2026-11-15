package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/smart"
)

// fakeCommands stands in for smartctl on the three self-test paths. It records what the
// endpoint asked for, because half of what these tests are for is the *arguments*: the
// device path, the transport override, the test kind, and the timeout the setting resolves
// to. The command itself is not what is under test — the endpoint's decisions are.
type fakeCommands struct {
	readFn  func() (smart.Result, error)
	startFn func() (smart.Result, error)
	abortFn func() (smart.Result, error)

	readCalls   int
	startCalls  int
	abortCalls  int
	lastTimeout time.Duration
	lastPath    string
	lastKind    smart.SelfTestKind
}

func (f *fakeCommands) Read(_ context.Context, timeout time.Duration, path, transport string) (smart.Result, error) {
	f.readCalls++
	f.lastTimeout, f.lastPath = timeout, path
	if f.readFn == nil {
		return smart.Result{}, nil
	}
	return f.readFn()
}

func (f *fakeCommands) Start(_ context.Context, timeout time.Duration, path, transport string, kind smart.SelfTestKind) (smart.Result, error) {
	f.startCalls++
	f.lastTimeout, f.lastPath, f.lastKind = timeout, path, kind
	if f.startFn == nil {
		return smart.Result{}, nil
	}
	return f.startFn()
}

func (f *fakeCommands) Abort(_ context.Context, timeout time.Duration, path, transport string) (smart.Result, error) {
	f.abortCalls++
	f.lastTimeout, f.lastPath = timeout, path
	if f.abortFn == nil {
		return smart.Result{}, nil
	}
	return f.abortFn()
}

// nvmeLog is a `smartctl --json` self-test log. A non-zero current operation is what makes
// the drive "running"; the completion percentage is turned into the percentage-remaining the
// model carries, which is the one field Running keys on for both transports.
func nvmeLog(current int, percent int) []byte {
	return []byte(`{"nvme_self_test_log":{` +
		`"current_self_test_operation":{"value":` + strconv.Itoa(current) + `,"string":"Short self-test"},` +
		`"current_self_test_completion_percent":` + strconv.Itoa(percent) + `,` +
		`"table":[]}}`)
}

func selfTestServer(t *testing.T, cmds *fakeCommands, opts Options) *Server {
	t.Helper()
	opts.SelfTestCommands = cmds
	srv, _, _, _ := newTestServer(t, opts)
	// A scan is the only way a device record comes into existence, and a self-test needs one:
	// the drive's last-seen path is how the endpoint addresses it.
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)
	return srv
}

// TestSelfTestStatusIsReadLive pins the one place the API reads a drive outside a scan: the
// status endpoint returns what the drive says now, not what the last snapshot stored.
func TestSelfTestStatusIsReadLive(t *testing.T) {
	cmds := &fakeCommands{readFn: func() (smart.Result, error) {
		return smart.Result{Stdout: nvmeLog(1, 40)}, nil
	}}
	srv := selfTestServer(t, cmds, Options{})

	rec := do(t, srv, http.MethodGet, "/api/devices/S1/selftest", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if cmds.readCalls != 1 {
		t.Errorf("read calls = %d, want 1 — the status must not come from the snapshot", cmds.readCalls)
	}
	if cmds.lastPath != "/dev/sda" {
		t.Errorf("read path = %q, want the drive's own node", cmds.lastPath)
	}

	var body struct {
		Logs *struct {
			SelfTestSupported *bool `json:"self_test_supported"`
		} `json:"logs"`
		Running *struct {
			Type      string `json:"type"`
			Remaining *int   `json:"remaining"`
		} `json:"running"`
	}
	decodeBody(t, rec, &body)
	if body.Running == nil {
		t.Fatal("no running test reported, but the drive is mid-test")
	}
	if body.Running.Remaining == nil || *body.Running.Remaining != 60 {
		t.Errorf("remaining = %v, want 60 (100 - 40 percent complete)", body.Running.Remaining)
	}
}

func TestSelfTestStatusForUnknownDevice(t *testing.T) {
	srv := selfTestServer(t, &fakeCommands{}, Options{})

	rec := do(t, srv, http.MethodGet, "/api/devices/NOPE/selftest", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestStartSelfTestRejectsUnknownKind checks the kind is validated *before* anything reaches a
// drive: a typo must not become an attempt to write to hardware.
func TestStartSelfTestRejectsUnknownKind(t *testing.T) {
	cmds := &fakeCommands{}
	srv := selfTestServer(t, cmds, Options{})

	rec := do(t, srv, http.MethodPost, "/api/devices/S1/selftest", `{"kind":"thorough"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if cmds.startCalls != 0 {
		t.Errorf("start was called %d times for an invalid kind, want 0", cmds.startCalls)
	}
}

// TestStartSelfTestPassesTheDriveRefusalThrough covers the 502: the request was well formed and
// the drive would not take it, so smartctl's own words are the only useful answer.
func TestStartSelfTestPassesTheDriveRefusalThrough(t *testing.T) {
	cmds := &fakeCommands{startFn: func() (smart.Result, error) {
		return smart.Result{}, errors.New("SMART command failed: the drive refused the test")
	}}
	srv := selfTestServer(t, cmds, Options{})

	rec := do(t, srv, http.MethodPost, "/api/devices/S1/selftest", `{"kind":"short"}`, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "refused the test") {
		t.Errorf("body = %s, want the drive's own reason", rec.Body.String())
	}
	if cmds.lastKind != smart.SelfTestShort {
		t.Errorf("kind = %q, want short", cmds.lastKind)
	}
}

func TestStartSelfTestAcknowledges(t *testing.T) {
	cmds := &fakeCommands{startFn: func() (smart.Result, error) {
		return smart.Result{Stdout: []byte(`{"smartctl":{"messages":[{"string":"Self-test started"}]}}`)}, nil
	}}
	srv := selfTestServer(t, cmds, Options{})

	rec := do(t, srv, http.MethodPost, "/api/devices/S1/selftest", `{"kind":"long"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	decodeBody(t, rec, &body)
	if body["status"] != "started" || body["type"] != "long" {
		t.Errorf("body = %v, want status=started type=long", body)
	}
	if cmds.lastKind != smart.SelfTestLong {
		t.Errorf("kind = %q, want long", cmds.lastKind)
	}
}

// TestAbortReportsWhetherTheTestActuallyStopped is the bridge case (`FEATURES.md` A20): a
// bridge that does not pass the abort through accepts the command and carries on testing, so
// "the abort was sent" and "the test stopped" have to be reported as different facts.
func TestAbortReportsWhetherTheTestActuallyStopped(t *testing.T) {
	for _, tc := range []struct {
		name        string
		readback    []byte
		wantStopped bool
	}{
		{
			name:        "stopped",
			readback:    nvmeLog(0, 0),
			wantStopped: true,
		},
		{
			name:        "still running behind a bridge",
			readback:    nvmeLog(1, 15),
			wantStopped: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmds := &fakeCommands{
				abortFn: func() (smart.Result, error) { return smart.Result{}, nil },
				readFn:  func() (smart.Result, error) { return smart.Result{Stdout: tc.readback}, nil },
			}
			srv := selfTestServer(t, cmds, Options{})

			rec := do(t, srv, http.MethodPost, "/api/devices/S1/selftest/abort", "", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Status  string `json:"status"`
				Stopped *bool  `json:"stopped"`
				Running *struct {
					Remaining *int `json:"remaining"`
				} `json:"running"`
			}
			decodeBody(t, rec, &body)
			if body.Status != "aborted" {
				t.Errorf("status = %q, want aborted", body.Status)
			}
			if body.Stopped == nil {
				t.Fatal("no stopped field: the page cannot tell whether the test ended")
			}
			if *body.Stopped != tc.wantStopped {
				t.Errorf("stopped = %v, want %v", *body.Stopped, tc.wantStopped)
			}
			// When it is still running the entry comes back too, so the page can show what is
			// running rather than a contradiction it has to explain.
			if !tc.wantStopped && body.Running == nil {
				t.Error("no running entry reported for a test that is still going")
			}
			if cmds.abortCalls != 1 || cmds.readCalls != 1 {
				t.Errorf("abort=%d read=%d, want one of each", cmds.abortCalls, cmds.readCalls)
			}
		})
	}
}

// TestSelfTestUsesTheResolvedTimeout is the regression guard for the option that exists so
// these three calls honour SMARTSERVE_COMMAND_TIMEOUT instead of the shipped default: the API
// makes them itself, outside the scan pipeline that the setting otherwise reaches.
func TestSelfTestUsesTheResolvedTimeout(t *testing.T) {
	cmds := &fakeCommands{}
	srv := selfTestServer(t, cmds, Options{
		CommandTimeout: func(context.Context) time.Duration { return 42 * time.Second },
	})

	do(t, srv, http.MethodGet, "/api/devices/S1/selftest", "", nil)
	if cmds.lastTimeout != 42*time.Second {
		t.Errorf("timeout = %v, want 42s from the setting", cmds.lastTimeout)
	}
}

// TestSelfTestFailureCarriesAReference checks the failure path uses the same 500 shape as the
// rest of the API rather than handing the store's words to a browser (ADR-031).
func TestSelfTestFailureCarriesAReference(t *testing.T) {
	cmds := &fakeCommands{readFn: func() (smart.Result, error) {
		return smart.Result{}, errors.New("open /data/smartserve.db: permission denied")
	}}
	srv := selfTestServer(t, cmds, Options{})

	rec := do(t, srv, http.MethodGet, "/api/devices/S1/selftest", "", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if strings.Contains(body["error"], "smartserve.db") {
		t.Errorf("body leaks the path: %s", body["error"])
	}
	if !strings.Contains(body["error"], "ref") {
		t.Errorf("body has no reference: %s", body["error"])
	}
}
