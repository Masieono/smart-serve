package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// This file holds the HTTP push adapters. Each implements Sender, so adding a
// provider never touches the scan pipeline (docs/NOTIFICATIONS.md).

// --- gotify -------------------------------------------------------------------

// gotifySender posts to a self-hosted Gotify server.
type gotifySender struct{ client *http.Client }

// NewGotify returns the Gotify channel sender.
func NewGotify(client *http.Client) Sender {
	if client == nil {
		client = newHTTPClient()
	}
	return gotifySender{client: client}
}

func (gotifySender) Type() string { return "gotify" }

func (s gotifySender) ValidateConfig(cfg map[string]any) error {
	if stringField(cfg, "server") == "" {
		return errors.New("server is required")
	}
	if u, err := url.Parse(stringField(cfg, "server")); err != nil ||
		(u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("server must be an http(s) URL")
	}
	if stringField(cfg, "token") == "" {
		return errors.New("app token is required")
	}
	return nil
}

func (s gotifySender) Send(ctx context.Context, cfg map[string]any, ev Event) error {
	if err := s.ValidateConfig(cfg); err != nil {
		return err
	}
	endpoint := strings.TrimRight(stringField(cfg, "server"), "/") + "/message?token=" +
		url.QueryEscape(stringField(cfg, "token"))
	payload, err := json.Marshal(map[string]any{
		"title":    ev.Title(),
		"message":  ev.Body(),
		"priority": gotifyPriority(ev.Priority),
	})
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	return doRequest(ctx, s.client, func() (*http.Request, error) {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
}

// gotifyPriority maps onto Gotify's 0-10 scale (docs/NOTIFICATIONS.md).
func gotifyPriority(p Priority) int {
	switch p {
	case PriorityHigh:
		return 9
	case PriorityMedium:
		return 6
	default:
		return 3
	}
}

// --- telegram -----------------------------------------------------------------

// telegramSender posts through the Telegram Bot API.
type telegramSender struct{ client *http.Client }

// NewTelegram returns the Telegram channel sender.
func NewTelegram(client *http.Client) Sender {
	if client == nil {
		client = newHTTPClient()
	}
	return telegramSender{client: client}
}

func (telegramSender) Type() string { return "telegram" }

func (s telegramSender) ValidateConfig(cfg map[string]any) error {
	if stringField(cfg, "token") == "" {
		return errors.New("bot token is required")
	}
	if stringField(cfg, "chat_id") == "" {
		return errors.New("chat id is required")
	}
	return nil
}

func (s telegramSender) Send(ctx context.Context, cfg map[string]any, ev Event) error {
	if err := s.ValidateConfig(cfg); err != nil {
		return err
	}
	// The base is overridable so a self-hosted Bot API server works, and so the
	// transport can be tested without reaching the real Telegram.
	base := strings.TrimRight(stringField(cfg, "api_base"), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	endpoint := base + "/bot" + stringField(cfg, "token") + "/sendMessage"

	payload := map[string]any{
		"chat_id": stringField(cfg, "chat_id"),
		"text":    ev.Body(),
	}
	// A topic id turns a busy group into something usable.
	if thread := stringField(cfg, "thread_id"); thread != "" {
		if id, err := strconv.Atoi(thread); err == nil {
			payload["message_thread_id"] = id
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	return doRequest(ctx, s.client, func() (*http.Request, error) {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
}

// --- discord ------------------------------------------------------------------

// discordSender posts to a Discord channel webhook.
type discordSender struct{ client *http.Client }

// NewDiscord returns the Discord channel sender.
func NewDiscord(client *http.Client) Sender {
	if client == nil {
		client = newHTTPClient()
	}
	return discordSender{client: client}
}

func (discordSender) Type() string { return "discord" }

func (s discordSender) ValidateConfig(cfg map[string]any) error {
	raw := stringField(cfg, "webhook_url")
	if raw == "" {
		return errors.New("webhook URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("webhook URL must be an http(s) URL")
	}
	return nil
}

func (s discordSender) Send(ctx context.Context, cfg map[string]any, ev Event) error {
	if err := s.ValidateConfig(cfg); err != nil {
		return err
	}
	// An embed renders far better than a wall of plain text: a coloured bar carries
	// the priority, and the fields stay aligned in Discord's own layout.
	payload := map[string]any{
		"embeds": []map[string]any{{
			"title":       ev.Title(),
			"description": ev.Body(),
			"color":       discordColor(ev.Priority),
			"timestamp":   timeFromMillis(ev.ScanTS),
		}},
	}
	if username := stringField(cfg, "username"); username != "" {
		payload["username"] = username
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	raw := stringField(cfg, "webhook_url")

	return doRequest(ctx, s.client, func() (*http.Request, error) {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, raw, bytes.NewReader(body))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
}

// discordColor is the embed accent, matching the app's health colours.
func discordColor(p Priority) int {
	switch p {
	case PriorityHigh:
		return 0xf85149 // red
	case PriorityMedium:
		return 0xd29922 // amber
	default:
		return 0x2ea043 // green
	}
}
