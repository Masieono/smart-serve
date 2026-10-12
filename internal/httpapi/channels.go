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
}

func toChannelJSON(c store.Channel) channelJSON {
	return channelJSON{
		ID: c.ID, Type: c.Type, Label: c.Label,
		Config: rawOrNull(c.Config), Enabled: c.Enabled, Events: rawOrNull(c.Events),
	}
}

type channelBody struct {
	Type    string          `json:"type"`
	Label   string          `json:"label"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
	Events  json.RawMessage `json:"events"`
}

func (b channelBody) toStore(id int64) store.Channel {
	c := store.Channel{
		ID: id, Type: b.Type, Label: b.Label,
		Config: "{}", Events: "[]", Enabled: true,
	}
	if len(b.Config) > 0 {
		c.Config = string(b.Config)
	}
	if len(b.Events) > 0 {
		c.Events = string(b.Events)
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
	if msg := s.validateChannelConfig(body.Type, body.Config); msg != "" {
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
	if msg := s.validateChannelConfig(merged.Type, json.RawMessage(merged.Config)); msg != "" {
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
func (s *Server) validateChannelConfig(typ string, raw json.RawMessage) string {
	if s.notifier == nil || len(raw) == 0 {
		return ""
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "config must be a JSON object"
	}
	if err := s.notifier.ValidateConfig(typ, cfg); err != nil {
		return err.Error()
	}
	return ""
}

// handleNotifications returns the delivery log plus the event and channel types
// the UI needs to render subscription checkboxes. The log is how an operator
// confirms an alert was really sent (ADR-008).
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
	})
}
