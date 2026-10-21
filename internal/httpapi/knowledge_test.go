package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masieono/smart-serve/internal/store"
)

// knowledgeStatus is what the settings page reads.
type knowledgeStatus struct {
	URL             string `json:"url"`
	BundledEntries  int    `json:"bundled_entries"`
	ResolvedEntries int    `json:"resolved_entries"`
	CacheStale      bool   `json:"cache_stale"`
	Cache           *struct {
		Entries   int    `json:"entries"`
		FetchedAt string `json:"fetched_at"`
		SourceURL string `json:"source_url"`
	} `json:"cache"`
	Overrides *struct {
		Entries int    `json:"entries"`
		Payload string `json:"payload"`
	} `json:"overrides"`
	Resolved []struct {
		Family string `json:"family"`
	} `json:"resolved"`
	Bundled string `json:"bundled"`
}

func getKnowledge(t *testing.T, srv *Server) knowledgeStatus {
	t.Helper()
	var out knowledgeStatus
	decodeBody(t, do(t, srv, http.MethodGet, "/api/knowledge", "", nil), &out)
	return out
}

// The bundled table is the floor: it works with no network, no settings and no edits, which is what
// makes the feature useful on the first page load.
func TestKnowledgeStartsFromTheBundledTable(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	got := getKnowledge(t, srv)
	if got.BundledEntries < 5 {
		t.Errorf("bundled entries = %d, want the shipped table", got.BundledEntries)
	}
	if got.ResolvedEntries != got.BundledEntries {
		t.Errorf("resolved = %d with no other layers, want %d", got.ResolvedEntries, got.BundledEntries)
	}
	if got.URL == "" {
		t.Error("no refresh URL reported")
	}
	if got.Cache != nil || got.Overrides != nil {
		t.Errorf("layers reported with nothing stored: cache=%v overrides=%v", got.Cache, got.Overrides)
	}
	// The shipped document comes back verbatim, which is how a user starts their own copy to point
	// the URL at.
	if !strings.Contains(got.Bundled, `"version"`) {
		t.Error("the bundled document is not JSON")
	}
}

// A drive the table knows about carries the rating and its source. The sample device is a Samsung
// 870 EVO, which the bundled table covers — so this also proves the shipped data actually matches what
// smartctl reports, which is the one thing about a knowledge base that has to be right.
func TestAKnownDriveCarriesItsRating(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	var list struct {
		Devices []deviceJSON `json:"devices"`
	}
	decodeBody(t, do(t, srv, http.MethodGet, "/api/devices", "", nil), &list)
	if len(list.Devices) != 1 {
		t.Fatalf("got %d devices", len(list.Devices))
	}
	k := list.Devices[0].Knowledge
	if k == nil {
		t.Fatal("the bundled table does not know a Samsung 870 EVO, which is what it ships for")
	}
	if k.Family != "Samsung SSD 870 EVO" {
		t.Errorf("family = %q", k.Family)
	}
	// The sample drive is 1 TB, so the per-capacity figure applies.
	if k.RatedTBW == nil || *k.RatedTBW != 600 {
		t.Errorf("rated TBW = %v, want 600 for a 1 TB 870 EVO", k.RatedTBW)
	}
	if k.Source == "" {
		t.Error("no source: a figure nobody can check is a figure nobody should believe")
	}

	// And the detail endpoint carries the same lookup, from the same layers.
	var detail struct {
		Device deviceJSON `json:"device"`
	}
	decodeBody(t, do(t, srv, http.MethodGet, "/api/devices/S1", "", nil), &detail)
	if detail.Device.Knowledge == nil || detail.Device.Knowledge.Family != k.Family {
		t.Errorf("detail knowledge = %+v, want the same lookup", detail.Device.Knowledge)
	}
}

func TestADriveTheTableDoesNotKnowCarriesNothing(t *testing.T) {
	srv, _, runner, _ := newTestServer(t, Options{})
	devices := sampleDevices()
	// Both the discovered model and the one smartctl reported: the store takes one of them, and a
	// test that changed only one would pass for the wrong reason.
	devices[0].Discovered.Model = "Some Unlisted Drive 9000"
	devices[0].Data.Model = "Some Unlisted Drive 9000"
	runner.devices = devices
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)

	var list struct {
		Devices []deviceJSON `json:"devices"`
	}
	decodeBody(t, do(t, srv, http.MethodGet, "/api/devices", "", nil), &list)
	// Nil rather than an empty object: "we know nothing about this model" is an answer, and an empty
	// lookup would render as a rating of nothing.
	if list.Devices[0].Knowledge != nil {
		t.Errorf("knowledge = %+v, want nothing", list.Devices[0].Knowledge)
	}
}

// The user's overrides win over everything, and are validated before they are stored — because the
// error goes back to the person who typed it, and that is the whole reason the editor exists rather
// than a second file to edit by hand.
func TestOverridesWinAndAreValidated(t *testing.T) {
	srv, _, _, _ := newTestServer(t, Options{})

	// A typo is refused, with the reason.
	rec := do(t, srv, http.MethodPut, "/api/knowledge/overrides",
		`{"payload":"{\"version\":1,\"drives\":[{\"family\":\"My Drive\",\"rated_tbw\":0}]}"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be positive") {
		t.Errorf("the error does not say what is wrong: %s", rec.Body.String())
	}
	// And nothing was stored, so a bad save cannot break the page.
	if getKnowledge(t, srv).Overrides != nil {
		t.Error("an invalid document was stored anyway")
	}

	// A valid one is stored and wins.
	rec = do(t, srv, http.MethodPut, "/api/knowledge/overrides",
		`{"payload":"{\"version\":1,\"drives\":[{\"family\":\"Samsung SSD 870 EVO\",\"rated_tbw\":1234}]}"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := getKnowledge(t, srv)
	if got.Overrides == nil || got.Overrides.Entries != 1 {
		t.Fatalf("overrides = %+v", got.Overrides)
	}
	// Replaced whole-entry rather than merged field by field, so the entry count is unchanged: an
	// override substitutes an entry rather than adding one.
	if got.ResolvedEntries != got.BundledEntries {
		t.Errorf("resolved = %d, want %d", got.ResolvedEntries, got.BundledEntries)
	}

	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)
	var detail struct {
		Device deviceJSON `json:"device"`
	}
	decodeBody(t, do(t, srv, http.MethodGet, "/api/devices/S1", "", nil), &detail)
	if k := detail.Device.Knowledge; k == nil || k.RatedTBW == nil || *k.RatedTBW != 1234 {
		t.Errorf("knowledge = %+v, want the override to win", k)
	}

	// An empty payload clears the layer rather than storing an empty document, which would fail its
	// own validation.
	if rec := do(t, srv, http.MethodPut, "/api/knowledge/overrides", `{"payload":"   "}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d", rec.Code)
	}
	if getKnowledge(t, srv).Overrides != nil {
		t.Error("the overrides layer was not cleared")
	}
}

// A refresh is a button, and it stores what it fetched — after parsing it, so a bad file cannot
// replace a working layer with something the app cannot read.
func TestRefreshStoresWhatItFetched(t *testing.T) {
	document := `{"version":1,"updated":"2026-11-01","drives":[
		{"family":"Samsung SSD 870 EVO","rated_tbw_by_capacity_gb":{"1000":700},"source":"test"},
		{"family":"A Drive Nobody Ships","rated_tbw":99,"source":"test"}]}`

	var served string
	status := http.StatusOK
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(served))
	}))
	defer upstream.Close()

	srv, st, _, _ := newTestServer(t, Options{})
	if err := st.SetSettings(context.Background(), map[string]string{"knowledge_url": upstream.URL}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}

	// A bad file is refused, and nothing is stored — so the bundled layer is untouched.
	served = `{"version":1,"drives":[{"family":"Bad","rated_tbw":1}]}` // family too short to match on
	if rec := do(t, srv, http.MethodPost, "/api/knowledge/refresh", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got := getKnowledge(t, srv); got.Cache != nil {
		t.Error("a document that failed to parse was stored anyway")
	}

	// A good one is stored, and adds to what the app knows.
	served = document
	rec := do(t, srv, http.MethodPost, "/api/knowledge/refresh", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := getKnowledge(t, srv)
	if got.Cache == nil || got.Cache.Entries != 2 {
		t.Fatalf("cache = %+v", got.Cache)
	}
	if got.Cache.FetchedAt == "" {
		t.Error("the fetched copy has no timestamp, so its age cannot be shown")
	}
	if got.Cache.SourceURL != upstream.URL {
		t.Errorf("source = %q, want the URL it came from", got.Cache.SourceURL)
	}
	// One entry replaced a bundled one and one is new.
	if got.ResolvedEntries != got.BundledEntries+1 {
		t.Errorf("resolved = %d, want %d", got.ResolvedEntries, got.BundledEntries+1)
	}

	// The fetched figure now applies, superseding the bundled one.
	do(t, srv, http.MethodPost, "/api/scan?wait=1", "", nil)
	var detail struct {
		Device deviceJSON `json:"device"`
	}
	decodeBody(t, do(t, srv, http.MethodGet, "/api/devices/S1", "", nil), &detail)
	if k := detail.Device.Knowledge; k == nil || k.RatedTBW == nil || *k.RatedTBW != 700 {
		t.Errorf("knowledge = %+v, want the fetched 700", k)
	}

	// A URL that answers with an error says so, and the stored copy stays.
	status = http.StatusNotFound
	if rec := do(t, srv, http.MethodPost, "/api/knowledge/refresh", "", nil); rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 for an upstream error", rec.Code)
	}
	if getKnowledge(t, srv).Cache == nil {
		t.Error("a failed refresh discarded the stored copy")
	}

	// Changing the URL marks the stored copy stale, which is what a reader needs to know before
	// trusting its age.
	if err := st.SetSettings(context.Background(),
		map[string]string{"knowledge_url": upstream.URL + "/moved"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if !getKnowledge(t, srv).CacheStale {
		t.Error("a copy fetched from a different URL is not reported as stale")
	}

	// And discarding it is how a refresh is undone.
	if rec := do(t, srv, http.MethodDelete, "/api/knowledge/cache", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d", rec.Code)
	}
	if got := getKnowledge(t, srv); got.Cache != nil {
		t.Error("the cached layer was not cleared")
	}
}

// A URL that is not http(s) is refused before anything is fetched, because the setting is free text
// and a typo should not become a request.
func TestRefreshRefusesANonHTTPURL(t *testing.T) {
	srv, st, _, _ := newTestServer(t, Options{})
	if err := st.SetSettings(context.Background(),
		map[string]string{"knowledge_url": "file:///etc/passwd"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}

	if rec := do(t, srv, http.MethodPost, "/api/knowledge/refresh", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// The store layer is two rows in one table, and a document is written verbatim so the overrides editor
// can hand back exactly what it was given.
func TestKnowledgeDocsRoundTrip(t *testing.T) {
	_, st, _, _ := newTestServer(t, Options{})
	ctx := context.Background()

	if doc, err := st.GetKnowledgeDoc(ctx, store.KnowledgeOverrides); err != nil || doc != nil {
		t.Fatalf("doc = %v, %v; want nil and no error", doc, err)
	}
	if err := st.PutKnowledgeDoc(ctx, store.KnowledgeOverrides, `{"version":1}`, ""); err != nil {
		t.Fatalf("PutKnowledgeDoc: %v", err)
	}
	doc, err := st.GetKnowledgeDoc(ctx, store.KnowledgeOverrides)
	if err != nil || doc == nil || doc.Payload != `{"version":1}` {
		t.Fatalf("doc = %+v, %v", doc, err)
	}
	// Overrides have no source, and storing them must not invent one.
	if doc.SourceURL != "" || !doc.FetchedAt.IsZero() {
		t.Errorf("overrides carry a source: %+v", doc)
	}
}
