package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/store"
)

func exportBundle(t *testing.T, srv *Server) configBundle {
	t.Helper()
	rec := do(t, srv, http.MethodGet, "/api/config/export", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body %s", rec.Code, rec.Body.String())
	}
	var bundle configBundle
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	return bundle
}

func importBundle(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, srv, http.MethodPost, "/api/config/import", body, nil)
}

func labelDrive(t *testing.T, st *store.Store, serial, name, bay string) {
	t.Helper()
	if err := st.SetNotes(context.Background(), store.Notes{
		Serial: serial, Name: name, Bay: bay, Tags: "tank", Notes: "bought 2024-03",
	}); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
}

// The user's own knowledge entries travel in the bundle (`FEATURES.md` A21): they are hand-typed
// figures for drives nobody else has written down, and a host migration that dropped them would drop
// the only copy.
//
// The *fetched* layer deliberately does not travel. It is a cache of a URL the settings already
// carry, and a bundle holding a stale copy of somebody else's file would be worse than re-fetching it.
func TestTheBundleCarriesKnowledgeOverridesButNotTheCache(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	ctx := context.Background()

	overrides := `{"version":1,"drives":[{"family":"My Drive 9000","rated_tbw":600,"source":"the box"}]}`
	if err := st.PutKnowledgeDoc(ctx, store.KnowledgeOverrides, overrides, ""); err != nil {
		t.Fatalf("PutKnowledgeDoc: %v", err)
	}
	if err := st.PutKnowledgeDoc(ctx, store.KnowledgeCache, `{"version":1,"drives":[{"family":"Cached Drive","rated_tbw":1}]}`, "http://example.test/k.json"); err != nil {
		t.Fatalf("PutKnowledgeDoc: %v", err)
	}

	got := exportBundle(t, srv)
	if got.Knowledge != overrides {
		t.Errorf("knowledge = %q, want the overrides verbatim", got.Knowledge)
	}
	if strings.Contains(got.Knowledge, "Cached Drive") {
		t.Error("the fetched layer travelled in the bundle; it is a cache, not configuration")
	}

	// And the import applies them, so a restore brings the figures back.
	fresh, freshStore, _, _ := newTestServer(t, Options{})
	if rec := importBundle(t, fresh, recode(t, got)); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", rec.Code, rec.Body.String())
	}
	doc, err := freshStore.GetKnowledgeDoc(ctx, store.KnowledgeOverrides)
	if err != nil || doc == nil || doc.Payload != overrides {
		t.Fatalf("after import: doc = %+v, %v", doc, err)
	}

	// A bundle whose knowledge entries are unusable is refused, and nothing is stored — a bad layer
	// would degrade every drive page rather than failing loudly here.
	bad := configBundle{SmartServe: bundleVersion, Knowledge: `{"version":1,"drives":[{"family":"Bad"}]}`}
	rec := importBundle(t, fresh, recode(t, bad))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not usable") {
		t.Errorf("the error does not say what is wrong: %s", rec.Body.String())
	}
	if doc, _ := freshStore.GetKnowledgeDoc(ctx, store.KnowledgeOverrides); doc == nil || doc.Payload != overrides {
		t.Error("a refused import changed the stored entries")
	}
}

// recode marshals a bundle back to JSON, so a test can post what the export produced.
func recode(t *testing.T, bundle configBundle) string {
	t.Helper()
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	return string(data)
}

func appliedCounts(t *testing.T, rec *httptest.ResponseRecorder) map[string]int {
	t.Helper()
	var got struct {
		Applied map[string]int `json:"applied"`
	}
	decodeBody(t, rec, &got)
	return got.Applied
}

// The whole point of C7: labels survive a move to another host. This is that move in one test —
// one server exports, a second server on a fresh database imports, and the labels are there.
func TestLabelsSurviveAHostMigration(t *testing.T) {
	source, sourceStore, _, _ := newTestServer(t, Options{})
	labelDrive(t, sourceStore, "S1", "parity", "bay 4")

	bundle := exportBundle(t, source)
	if len(bundle.Devices) != 1 {
		t.Fatalf("exported %d label sets, want 1", len(bundle.Devices))
	}
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}

	target, targetStore, _, _ := newTestServer(t, Options{})
	if rec := importBundle(t, target, string(body)); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	notes, err := targetStore.GetNotes(context.Background(), "S1")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if notes == nil {
		t.Fatal("no labels after the import")
	}
	if notes.Name != "parity" || notes.Bay != "bay 4" || notes.Tags != "tank" {
		t.Errorf("labels = %+v", notes)
	}
}

// A backup file is exactly the thing that gets emailed around, so it must not carry tokens.
func TestTheExportCarriesNoSecrets(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	if _, err := st.CreateChannel(context.Background(), store.Channel{
		Type: "ntfy", Label: "alerts", Enabled: true, Events: "[]",
		Config: `{"server":"https://ntfy.sh","topic":"t","token":"ntfy_SUPERSECRET"}`,
	}); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/config/export", "", nil)
	if strings.Contains(rec.Body.String(), "SUPERSECRET") {
		t.Error("the export contains a channel token")
	}
	// And it says why, in the file itself, so a bundle found later explains itself.
	if !strings.Contains(rec.Body.String(), "Channels are deliberately not included") {
		t.Error("the export does not say that channels were left out")
	}
}

// Handing this endpoint the wrong JSON file must say so, rather than applying nothing and
// reporting success.
func TestAForeignBundleIsRefused(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	for _, body := range []string{
		`{"hello":"world"}`,
		`{"smartserve":99,"devices":[]}`,
		`{"smartserve":0}`,
	} {
		if rec := importBundle(t, srv, body); rec.Code != http.StatusBadRequest {
			t.Errorf("import %s = %d, want 400", body, rec.Code)
		}
	}
}

// Merge, with the file winning for what it contains: a restore that deleted anything it did not
// recognise would be one you could not safely run twice.
func TestImportLeavesWhatTheFileDoesNotMentionAlone(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	labelDrive(t, st, "KEEP", "keep me", "bay 1")

	rec := importBundle(t, srv, `{"smartserve":1,"devices":[{"serial":"NEW","name":"imported"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	kept, err := st.GetNotes(context.Background(), "KEEP")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if kept == nil || kept.Name != "keep me" {
		t.Errorf("the drive the file did not mention lost its labels: %+v", kept)
	}
	added, err := st.GetNotes(context.Background(), "NEW")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if added == nil || added.Name != "imported" {
		t.Errorf("the imported drive has no labels: %+v", added)
	}
}

func TestImportReportsWhatItApplied(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	rec := importBundle(t, srv,
		`{"smartserve":1,"devices":[{"serial":"A"},{"serial":"B"}],"settings":{"theme":"dark"}}`)

	if got := appliedCounts(t, rec); got["devices"] != 2 || got["settings"] != 1 {
		t.Errorf("applied = %v, want 2 devices and 1 setting", got)
	}
}

// A device with no serial is not a device. Skipped rather than written as an empty row.
func TestADeviceWithNoSerialIsSkipped(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	rec := importBundle(t, srv, `{"smartserve":1,"devices":[{"serial":""},{"serial":"OK"}]}`)

	if got := appliedCounts(t, rec); got["devices"] != 1 {
		t.Errorf("applied devices = %d, want 1", got["devices"])
	}
	if notes, err := st.GetNotes(context.Background(), ""); err != nil || notes != nil {
		t.Errorf("an empty serial produced a row: %+v, %v", notes, err)
	}
}

// seedSchedule puts one schedule in the store so the import tests can tell "left alone" from
// "replaced".
func seedSchedule(t *testing.T, st *store.Store, label string) {
	t.Helper()
	every := int64(21600)
	if _, err := st.CreateSchedule(context.Background(), store.Schedule{
		Enabled: true, Kind: "interval", EverySeconds: &every, Label: label,
	}); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
}

// A labels-only bundle must not wipe the schedule list. That is the difference between a restore
// and a restore you have to think about.
func TestImportWithoutSchedulesLeavesThemAlone(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	seedSchedule(t, st, "every 6h")

	rec := importBundle(t, srv, `{"smartserve":1,"devices":[{"serial":"A"}]}`)
	if got := appliedCounts(t, rec); got["schedules"] != 0 {
		t.Errorf("applied schedules = %d, want 0", got["schedules"])
	}

	schedules, err := st.ListSchedules(context.Background())
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(schedules) != 1 || schedules[0].Label != "every 6h" {
		t.Errorf("schedules = %+v, want the existing one kept", schedules)
	}
}

// A file that *does* carry schedules replaces them: a schedule has no natural key, so importing the
// same file twice would otherwise leave duplicates.
func TestImportWithSchedulesReplacesThem(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	seedSchedule(t, st, "old")

	rec := importBundle(t, srv,
		`{"smartserve":1,"schedules":[{"kind":"cron","cron":"0 3 * * *","label":"nightly","enabled":true}]}`)
	if got := appliedCounts(t, rec); got["schedules"] != 1 {
		t.Errorf("applied schedules = %d, want 1", got["schedules"])
	}

	schedules, err := st.ListSchedules(context.Background())
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(schedules) != 1 || schedules[0].Label != "nightly" {
		t.Fatalf("schedules = %+v, want only the imported one", schedules)
	}
	// Bookkeeping is dropped: an imported schedule has never run here, and its next run is a time
	// the scheduler computes from the current clock. A null next run is also what makes it arm.
	if schedules[0].LastRun != nil || schedules[0].NextRun != nil {
		t.Errorf("imported schedule carries bookkeeping: %+v", schedules[0])
	}
}

// An empty array is a file that carries schedules and has none — different from a file that does
// not mention them at all.
func TestAnEmptyScheduleListIsRespected(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	seedSchedule(t, st, "old")

	importBundle(t, srv, `{"smartserve":1,"schedules":[]}`)

	schedules, err := st.ListSchedules(context.Background())
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(schedules) != 0 {
		t.Errorf("got %d schedules, want none — the file said so", len(schedules))
	}
}
