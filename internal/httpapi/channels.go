package httpapi

import (
	"encoding/json"
	"net/http"

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

// handleTestChannel sends a test event. Notification delivery lands with the
// notifications slice (docs/SCOPE.md), so this is an explicit 501 for now.
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
	writeError(w, http.StatusNotImplemented, "notification delivery is not implemented yet")
}
