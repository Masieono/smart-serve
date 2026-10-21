package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/knowledge"
	"github.com/masieono/smart-serve/internal/store"
)

// DefaultKnowledgeURL is where a fresh install looks for an updated table.
//
// It points at the file inside the repository rather than at a release asset, because that file **is**
// the bundled table: the app ships a copy of it and fetches the same bytes. Pointing somewhere else is
// one setting, which is the whole reason the refresh exists — a repository that goes quiet must not
// take the data with it.
const DefaultKnowledgeURL = "https://raw.githubusercontent.com/Masieono/smart-serve/main/internal/knowledge/base.json"

// knowledgeMaxBytes bounds a fetched document.
//
// A knowledge base is a few tens of kilobytes. The bound is not about this app's appetite but about
// what happens when the URL is wrong: without it, a mistyped address pointing at something enormous
// would be read into memory before anybody could notice.
const knowledgeMaxBytes = 4 << 20 // 4 MiB

// knowledgeFetchTimeout bounds one refresh.
//
// Generous, because it runs on a button press rather than in a path anyone is waiting on — and short
// enough that a black-holed address does not hold the handler open indefinitely.
const knowledgeFetchTimeout = 20 * time.Second

// resolveKnowledge builds the three-layer table for a request (`FEATURES.md` A21).
//
// **A layer that fails to load is skipped, not fatal**, and that is deliberate: the bundled table is
// the floor, so a corrupt cached copy or an override the user broke with a typo degrades to "we know
// less" instead of "the drive page does not load". The *editor* is where a typo is refused, with the
// parse error in front of the person who typed it — which is the only place a message about it is
// worth anything.
func (s *Server) resolveKnowledge(ctx context.Context) knowledge.Base {
	layers := make([]knowledge.Base, 0, 3)
	if bundled, err := knowledge.Embedded(); err == nil {
		layers = append(layers, bundled)
	}
	// Bundled first, then fetched, then the user's own edits — the order is the precedence.
	for _, kind := range []string{store.KnowledgeCache, store.KnowledgeOverrides} {
		doc, err := s.store.GetKnowledgeDoc(ctx, kind)
		if err != nil || doc == nil {
			continue
		}
		if parsed, perr := knowledge.Parse([]byte(doc.Payload)); perr == nil {
			layers = append(layers, parsed)
		}
	}
	return knowledge.Merge(layers...)
}

// lookupKnowledge resolves one drive against the three layers.
//
// Convenience for the handlers that carry a device payload, so none of them has to remember the
// argument order or resolve the base twice.
func (s *Server) lookupKnowledge(ctx context.Context, d store.DeviceRecord) knowledge.Lookup {
	return s.resolveKnowledge(ctx).Find(d.Model, d.ModelFamily, d.Firmware, d.CapacityBytes)
}

// knowledgeURL is where a refresh fetches from: the setting, or the documented default.
func (s *Server) knowledgeURL(ctx context.Context) string {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return DefaultKnowledgeURL
	}
	if configured := strings.TrimSpace(settings["knowledge_url"]); configured != "" {
		return configured
	}
	return DefaultKnowledgeURL
}

// knowledgeStatusJSON is the Settings view of the knowledge base.
type knowledgeStatusJSON struct {
	// URL is where a refresh would fetch from.
	URL string `json:"url"`
	// BundledEntries is how many families ship with this build.
	BundledEntries int `json:"bundled_entries"`
	// ResolvedEntries is how many the app actually has, after all three layers.
	ResolvedEntries int `json:"resolved_entries"`
	// Cache describes the fetched layer, absent when nothing has been fetched.
	Cache *knowledgeLayerJSON `json:"cache,omitempty"`
	// Overrides describes the user's own edits, absent when there are none.
	Overrides *knowledgeLayerJSON `json:"overrides,omitempty"`
	// CacheStale is true when the cached copy came from a different URL than the one now configured —
	// which is what a reader needs to know before trusting its age.
	CacheStale bool `json:"cache_stale,omitempty"`
	// Resolved is the merged table, so the settings page can show what the app knows without a second
	// request and without re-implementing the merge.
	Resolved []knowledge.Entry `json:"resolved"`
	// Bundled is the shipped document as text, for the "download the base" affordance — which is how a
	// user starts their own copy to point the URL at.
	Bundled string `json:"bundled"`
}

type knowledgeLayerJSON struct {
	Entries   int    `json:"entries"`
	Payload   string `json:"payload,omitempty"`
	FetchedAt string `json:"fetched_at,omitempty"`
	SourceURL string `json:"source_url,omitempty"`
}

func (s *Server) handleGetKnowledge(w http.ResponseWriter, r *http.Request) {
	out := knowledgeStatusJSON{URL: s.knowledgeURL(r.Context())}
	if bundled, err := knowledge.Embedded(); err == nil {
		out.BundledEntries = len(bundled.Drives)
	}
	out.Bundled = string(knowledge.EmbeddedBytes())
	out.Resolved = s.resolveKnowledge(r.Context()).Drives
	out.ResolvedEntries = len(out.Resolved)

	if doc, err := s.store.GetKnowledgeDoc(r.Context(), store.KnowledgeCache); err == nil && doc != nil {
		layer := &knowledgeLayerJSON{Payload: doc.Payload, SourceURL: doc.SourceURL}
		if !doc.FetchedAt.IsZero() {
			layer.FetchedAt = rfc3339(doc.FetchedAt)
		}
		if parsed, perr := knowledge.Parse([]byte(doc.Payload)); perr == nil {
			layer.Entries = len(parsed.Drives)
		}
		out.Cache = layer
		out.CacheStale = doc.SourceURL != "" && doc.SourceURL != out.URL
	}
	if doc, err := s.store.GetKnowledgeDoc(r.Context(), store.KnowledgeOverrides); err == nil && doc != nil {
		layer := &knowledgeLayerJSON{Payload: doc.Payload}
		if parsed, perr := knowledge.Parse([]byte(doc.Payload)); perr == nil {
			layer.Entries = len(parsed.Drives)
		}
		out.Overrides = layer
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRefreshKnowledge fetches the configured URL and stores it as the middle layer.
//
// **A button, never a timer.** The whole project is built on costing nothing while idle
// (`DESIGN.md` §2.1), and a knowledge base that quietly polled a URL would be the first thing to break
// that — for data that changes a few times a year. So the refresh happens when somebody asks for it,
// and the age of the copy is shown so they know whether to.
//
// A fetched document is **parsed before it is stored**, so a bad file leaves the previous layer in
// place rather than replacing something the app can read with something it cannot. The failure is also
// reported with the URL and the reason, because "the refresh did nothing" is not actionable.
func (s *Server) handleRefreshKnowledge(w http.ResponseWriter, r *http.Request) {
	url := s.knowledgeURL(r.Context())
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		writeError(w, http.StatusBadRequest, "knowledge_url must be an http(s) URL")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), knowledgeFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not build a request for that URL")
		return
	}
	// Some file hosts refuse a request that carries no user agent at all.
	req.Header.Set("User-Agent", "smart-serve/"+s.version)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not fetch "+url+": "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("%s answered %d", url, resp.StatusCode))
		return
	}

	// A document larger than the bound is truncated rather than refused, and the truncation shows up
	// as a parse error naming the byte it stopped at — which says more than "too big" would.
	body, err := io.ReadAll(io.LimitReader(resp.Body, knowledgeMaxBytes))
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not read the response: "+err.Error())
		return
	}
	parsed, perr := knowledge.Parse(body)
	if perr != nil {
		writeError(w, http.StatusBadRequest, url+" is not a usable knowledge document: "+perr.Error())
		return
	}
	if err := s.store.PutKnowledgeDoc(r.Context(), store.KnowledgeCache, string(body), url); err != nil {
		s.fail(w, "store knowledge cache", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": len(parsed.Drives), "url": url, "updated": parsed.Updated,
	})
}

// handlePutKnowledgeOverrides stores the user's own entries (`FEATURES.md` A21).
//
// Validated before it is stored, and the parse error is returned verbatim: this text was typed by the
// person reading the message, so it can name the field *and* the entry, and that is the whole reason
// the editor exists rather than a second file to edit by hand.
//
// An empty payload clears the layer rather than storing an empty document — the two mean the same
// thing, and one of them is a document that would fail its own validation.
func (s *Server) handlePutKnowledgeOverrides(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Payload string `json:"payload"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Payload) == "" {
		if err := s.store.ClearKnowledgeDoc(r.Context(), store.KnowledgeOverrides); err != nil {
			s.fail(w, "clear knowledge overrides", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": 0})
		return
	}
	parsed, err := knowledge.Parse([]byte(body.Payload))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.PutKnowledgeDoc(r.Context(), store.KnowledgeOverrides, body.Payload, ""); err != nil {
		s.fail(w, "store knowledge overrides", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": len(parsed.Drives)})
}

// handleClearKnowledgeCache discards the fetched layer, which is how a refresh is undone.
func (s *Server) handleClearKnowledgeCache(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearKnowledgeDoc(r.Context(), store.KnowledgeCache); err != nil {
		s.fail(w, "clear knowledge cache", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
}
