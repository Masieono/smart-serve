package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/masieono/smart-serve/internal/notify"
	"github.com/masieono/smart-serve/internal/store"
)

type channelJSON struct {
	ID      int64           `json:"id"`
	Type    string          `json:"type"`
	Label   string          `json:"label"`
	Config  json.RawMessage `json:"config"`
	Enabled bool            `json:"enabled"`
	Events  json.RawMessage `json:"events"`
	// ScopeGroups is the JSON array of tag names this channel is about (`FEATURES.md` B12). An
	// empty list means every group. Sent back so the editor shows what the channel will actually
	// receive rather than a blank that reads like "nothing".
	ScopeGroups json.RawMessage `json:"scope_groups"`
	// EnvPrefix is the prefix a deployment uses to override this channel's fields, derived
	// from the label (`ADR-022`). The UI shows it so the variable name never has to be
	// guessed, and so renaming a channel is visible rather than silently breaking the
	// override.
	EnvPrefix string `json:"env_prefix"`
	// Sources names where each overridden field's effective value comes from — "environment"
	// or "file". Fields with no override are absent.
	//
	// Provenance only, never the resolved values: handing a secret to a browser would leak an
	// environment secret into the page, and the next Save would write it into the database,
	// quietly promoting a secret that was meant to stay out of it.
	Sources map[string]notify.SecretSource `json:"sources,omitempty"`
}

func toChannelJSON(c store.Channel) channelJSON {
	out := channelJSON{
		ID: c.ID, Type: c.Type, Label: c.Label,
		Config: rawOrNull(c.Config), Enabled: c.Enabled, Events: rawOrNull(c.Events),
		ScopeGroups: rawOrNull(c.ScopeGroups),
		EnvPrefix:   notify.EnvPrefix(c.Label),
	}
	cfg := map[string]any{}
	if err := json.Unmarshal([]byte(c.Config), &cfg); err == nil {
		out.Sources = notify.Sources(c.Label, cfg)
	}
	return out
}

type channelBody struct {
	Type    string          `json:"type"`
	Label   string          `json:"label"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
	Events  json.RawMessage `json:"events"`
	// ScopeGroups is the tag names to narrow this channel to (`FEATURES.md` B12); an empty or
	// absent list means every group. Normalised by the store, which is also where the empty list
	// is defined as the only value that means "everything".
	ScopeGroups json.RawMessage `json:"scope_groups"`
}

func (b channelBody) toStore(id int64) store.Channel {
	c := store.Channel{
		ID: id, Type: b.Type, Label: b.Label,
		Config: "{}", Events: "[]", ScopeGroups: "[]", Enabled: true,
	}
	if len(b.Config) > 0 {
		c.Config = string(b.Config)
	}
	if len(b.Events) > 0 {
		c.Events = string(b.Events)
	}
	if len(b.ScopeGroups) > 0 {
		c.ScopeGroups = string(b.ScopeGroups)
	}
	if b.Enabled != nil {
		c.Enabled = *b.Enabled
	}
	return c
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.store.ListChannels(r.Context())
	if err != nil {
		s.fail(w, "list channels", err)
		return
	}
	out := make([]channelJSON, 0, len(channels))
	for _, c := range channels {
		out = append(out, toChannelJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var body channelBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if msg := s.validateChannelConfig(body.Type, body.Label, body.Config); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	id, err := s.store.CreateChannel(r.Context(), body.toStore(0))
	if err != nil {
		s.fail(w, "create channel", err)
		return
	}
	ch, err := s.store.GetChannel(r.Context(), id)
	if err != nil || ch == nil {
		s.fail(w, "create channel", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": toChannelJSON(*ch)})
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	existing, err := s.store.GetChannel(r.Context(), id)
	if err != nil {
		s.fail(w, "update channel", err)
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "unknown channel")
		return
	}

	var body channelBody
	if !decodeJSON(w, r, &body) {
		return
	}
	merged := body.toStore(id)
	if body.Type == "" {
		merged.Type = existing.Type
	}
	if body.Label == "" {
		merged.Label = existing.Label
	}
	if len(body.Config) == 0 {
		merged.Config = existing.Config
	}
	if len(body.Events) == 0 {
		merged.Events = existing.Events
	}
	if body.Enabled == nil {
		merged.Enabled = existing.Enabled
	}
	if msg := s.validateChannelConfig(merged.Type, merged.Label, json.RawMessage(merged.Config)); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateChannel(r.Context(), merged); err != nil {
		s.fail(w, "update channel", err)
		return
	}
	ch, _ := s.store.GetChannel(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"channel": toChannelJSON(*ch)})
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteChannel(r.Context(), id); err != nil {
		s.fail(w, "delete channel", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// handleTestChannel sends a synthetic event through the real delivery path, so
// an operator can confirm a channel works — and see the reason when it does not
// (docs/NOTIFICATIONS.md: test events bypass subscription filters and squelching).
func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ch, err := s.store.GetChannel(r.Context(), id)
	if err != nil {
		s.fail(w, "test channel", err)
		return
	}
	if ch == nil {
		writeError(w, http.StatusNotFound, "unknown channel")
		return
	}
	if s.notifier == nil {
		writeError(w, http.StatusNotImplemented, "notification delivery is not configured")
		return
	}
	outcome, err := s.notifier.SendTest(r.Context(), *ch, notify.TestEvent(s.version))
	if err != nil {
		// 502: the channel is fine, the destination is not.
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": string(outcome), "channel_id": id,
	})
}

// validateChannelConfig checks a config before it is saved, so a missing topic
// or a malformed URL is rejected at the point of editing rather than at 3 a.m.
// It returns a message, or "" when the config is acceptable (or unverifiable
// because notification delivery is not wired in).
func (s *Server) validateChannelConfig(typ, label string, raw json.RawMessage) string {
	if s.notifier == nil || len(raw) == 0 {
		return ""
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "config must be a JSON object"
	}
	// Validate what will actually be *sent*, not what is on screen. A token supplied by the
	// environment or a mounted file is absent from the editor's form, so validating the form
	// alone would reject the very save that makes the override usable.
	if err := notify.ApplyOverrides(label, cfg); err != nil {
		return err.Error()
	}
	if err := s.notifier.ValidateConfig(typ, cfg); err != nil {
		return err.Error()
	}
	return ""
}

// handleNotifications returns the delivery log plus the event and channel types
// the UI needs to render subscription checkboxes. The log is how an operator
// confirms an alert was really sent (ADR-008).
// handleTestChannelConfig sends a test through a channel described in the request body
// rather than a stored one.
//
// The editor uses this so that Test proves what is on screen — including edits that have
// not been saved — and so that a failed test can be corrected and retried without the
// dialog closing and having to be reopened.
func (s *Server) handleTestChannelConfig(w http.ResponseWriter, r *http.Request) {
	var body channelBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if msg := s.validateChannelConfig(body.Type, body.Label, body.Config); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if s.notifier == nil {
		writeError(w, http.StatusNotImplemented, "notification delivery is not configured")
		return
	}
	// id 0: an unsaved draft. SendTest still delivers it, but records nothing.
	outcome, err := s.notifier.SendTest(r.Context(), body.toStore(0), notify.TestEvent(s.version))
	if err != nil {
		// 502: the settings are well-formed, the destination is not.
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": string(outcome)})
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.store.RecentNotifications(r.Context(), limit)
	if err != nil {
		s.fail(w, "notifications", err)
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"id":         e.ID,
			"channel_id": e.ChannelID,
			"event_type": e.EventType,
			"event_key":  e.EventKey,
			"sent_at":    rfc3339(e.SentAt),
			"status":     e.Status,
			"detail":     e.Detail,
		})
	}

	channelTypes := []string{}
	if s.notifier != nil {
		channelTypes = s.notifier.Types()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": out,
		"event_types":   notify.EventTypes,
		"channel_types": channelTypes,
		// The names of the channel override variables this deployment has set, so the editor
		// can mark a field as supplied rather than showing it empty — and can stop demanding
		// a value that is never going to be typed into the form. Names only: `secrets.go`.
		"channel_env_vars": notify.ChannelEnvVars(),
	})
}
