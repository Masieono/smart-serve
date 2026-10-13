package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// smtpStub is a minimal SMTP server: enough to exercise the real send path without
// pulling in a dependency. It records the envelope and the message so a test can
// assert on what actually went out.
type smtpStub struct {
	listener net.Listener

	mu   sync.Mutex
	from string
	to   []string
	data string
	auth bool
}

func newSMTPStub(t *testing.T) *smtpStub {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stub := &smtpStub{listener: ln}
	go stub.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return stub
}

func (s *smtpStub) addr() (string, int) {
	host, portStr, _ := net.SplitHostPort(s.listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func (s *smtpStub) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *smtpStub) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	say := func(line string) { _, _ = fmt.Fprintf(conn, "%s\r\n", line) }

	say("220 stub ready")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-stub")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			s.mu.Lock()
			s.auth = true
			s.mu.Unlock()
			say("235 authenticated")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.mu.Lock()
			s.from = strings.TrimSpace(line[len("MAIL FROM:"):])
			s.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			s.mu.Lock()
			s.to = append(s.to, strings.TrimSpace(line[len("RCPT TO:"):]))
			s.mu.Unlock()
			say("250 ok")
		case cmd == "DATA":
			say("354 send it")
			var body strings.Builder
			for {
				dataLine, derr := reader.ReadString('\n')
				if derr != nil {
					return
				}
				if strings.TrimSpace(dataLine) == "." {
					break
				}
				body.WriteString(dataLine)
			}
			s.mu.Lock()
			s.data = body.String()
			s.mu.Unlock()
			say("250 accepted")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (s *smtpStub) captured() (from string, to []string, data string, authed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.from, append([]string(nil), s.to...), s.data, s.auth
}

// --- gotify -------------------------------------------------------------------

func TestGotifySenderPostsToTheAppToken(t *testing.T) {
	var (
		mu      sync.Mutex
		gotPath string
		got     map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		mu.Lock()
		defer mu.Unlock()
		gotPath = r.URL.RequestURI()
		got = decoded
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewGotify(srv.Client())
	cfg := map[string]any{"server": srv.URL, "token": "apptok"}
	if err := sender.Send(context.Background(), cfg, Event{
		Type: EventHealthChange, Priority: PriorityHigh, DeviceName: "boot",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/message?token=apptok" {
		t.Errorf("path = %q, want the message endpoint with the app token", gotPath)
	}
	if got["priority"] != float64(9) {
		t.Errorf("priority = %v, want 9 for a high-priority event", got["priority"])
	}
	if !strings.Contains(fmt.Sprint(got["message"]), "Drive health changed") {
		t.Errorf("message = %v", got["message"])
	}
}

func TestGotifyPriorityMapping(t *testing.T) {
	cases := map[Priority]int{PriorityLow: 3, PriorityMedium: 6, PriorityHigh: 9}
	for priority, want := range cases {
		if got := gotifyPriority(priority); got != want {
			t.Errorf("gotifyPriority(%q) = %d, want %d", priority, got, want)
		}
	}
}

// --- telegram -----------------------------------------------------------------

func TestTelegramSenderPostsToTheBotAPI(t *testing.T) {
	var (
		mu      sync.Mutex
		gotPath string
		got     map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		mu.Lock()
		defer mu.Unlock()
		gotPath = r.URL.Path
		got = decoded
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewTelegram(srv.Client())
	cfg := map[string]any{
		"token": "bottok", "chat_id": "-100123", "thread_id": "7", "api_base": srv.URL,
	}
	if err := sender.Send(context.Background(), cfg, Event{
		Type: EventDeviceAppeared, DeviceName: "usb", Priority: PriorityLow,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/botbottok/sendMessage" {
		t.Errorf("path = %q, want the sendMessage endpoint with the bot token", gotPath)
	}
	if got["chat_id"] != "-100123" {
		t.Errorf("chat_id = %v", got["chat_id"])
	}
	// The thread id must be a number: Telegram rejects a string, which is exactly
	// what a text input would otherwise send.
	if got["message_thread_id"] != float64(7) {
		t.Errorf("message_thread_id = %v, want the number 7", got["message_thread_id"])
	}
	if !strings.Contains(fmt.Sprint(got["text"]), "Drive detected") {
		t.Errorf("text = %v", got["text"])
	}
}

func TestTelegramOmitsTheThreadWhenUnset(t *testing.T) {
	var (
		mu  sync.Mutex
		got map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		mu.Lock()
		defer mu.Unlock()
		got = decoded
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewTelegram(srv.Client())
	cfg := map[string]any{"token": "t", "chat_id": "-100", "api_base": srv.URL}
	if err := sender.Send(context.Background(), cfg, Event{Type: EventHealthChange}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if _, present := got["message_thread_id"]; present {
		t.Errorf("message_thread_id should be omitted when unset, got %v", got["message_thread_id"])
	}
}

func TestTelegramConfigValidation(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewTelegram(nil))

	if err := reg.ValidateConfig("telegram", map[string]any{"token": "t", "chat_id": "-100"}); err != nil {
		t.Errorf("ValidateConfig = %v, want nil", err)
	}
	for _, bad := range []map[string]any{
		{"chat_id": "-100"}, // no token
		{"token": "t"},      // no chat
		{},                  // neither
	} {
		if err := reg.ValidateConfig("telegram", bad); err == nil {
			t.Errorf("ValidateConfig(%v) should have failed", bad)
		}
	}
}

// --- discord ------------------------------------------------------------------

func TestDiscordSenderPostsAnEmbed(t *testing.T) {
	var (
		mu   sync.Mutex
		got  map[string]any
		path string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		mu.Lock()
		defer mu.Unlock()
		got = decoded
		path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sender := NewDiscord(srv.Client())
	cfg := map[string]any{"webhook_url": srv.URL + "/api/webhooks/1/abc", "username": "smart-serve"}
	if err := sender.Send(context.Background(), cfg, Event{
		Type: EventScanFailure, Priority: PriorityMedium, Error: "boom",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if path != "/api/webhooks/1/abc" {
		t.Errorf("path = %q, want the webhook path", path)
	}
	if got["username"] != "smart-serve" {
		t.Errorf("username = %v", got["username"])
	}
	embeds, ok := got["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("embeds = %v, want exactly one", got["embeds"])
	}
	embed := embeds[0].(map[string]any)
	if embed["color"] != float64(0xd29922) {
		t.Errorf("colour = %v, want the amber for a medium priority", embed["color"])
	}
	if !strings.Contains(fmt.Sprint(embed["description"]), "boom") {
		t.Errorf("description = %v", embed["description"])
	}
}

func TestDiscordColourByPriority(t *testing.T) {
	cases := map[Priority]int{PriorityLow: 0x2ea043, PriorityMedium: 0xd29922, PriorityHigh: 0xf85149}
	for priority, want := range cases {
		if got := discordColor(priority); got != want {
			t.Errorf("discordColor(%q) = %#x, want %#x", priority, got, want)
		}
	}
}

// --- smtp ---------------------------------------------------------------------

func TestSMTPSenderDeliversAMessage(t *testing.T) {
	stub := newSMTPStub(t)
	host, port := stub.addr()

	sender := NewSMTP()
	cfg := map[string]any{
		"host": host, "port": strconv.Itoa(port),
		"username": "user", "password": "pass",
		"from": "smart-serve@example.com",
		"to":   "ops@example.com, second@example.com",
	}
	err := sender.Send(context.Background(), cfg, Event{
		Type: EventHealthChange, Priority: PriorityHigh,
		DeviceName: "boot", DeviceModel: "Samsung SSD 870 EVO",
		OldHealth: "good", NewHealth: "caution", Rationale: "5 pending sectors",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	from, to, data, authed := stub.captured()
	if from != "<smart-serve@example.com>" {
		t.Errorf("envelope from = %q", from)
	}
	if len(to) != 2 || to[0] != "<ops@example.com>" || to[1] != "<second@example.com>" {
		t.Errorf("envelope to = %v, want both recipients", to)
	}
	if !authed {
		t.Error("the sender should authenticate when a username is configured")
	}
	for _, want := range []string{"Subject:", "MIME-Version:", "quoted-printable", "good", "5 pending sectors"} {
		if !strings.Contains(data, want) {
			t.Errorf("message missing %q:\n%s", want, data)
		}
	}
	// The message must use CRLF line endings; a bare LF is not legal SMTP.
	if strings.Contains(strings.ReplaceAll(data, "\r\n", ""), "\n") {
		t.Error("message contains bare LF line endings")
	}
}

func TestSMTPSenderSkipsAuthWithoutAUsername(t *testing.T) {
	stub := newSMTPStub(t)
	host, port := stub.addr()

	sender := NewSMTP()
	cfg := map[string]any{
		"host": host, "port": strconv.Itoa(port),
		"from": "a@example.com", "to": "b@example.com",
	}
	if err := sender.Send(context.Background(), cfg, Event{Type: EventScanFailure, Error: "boom"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, _, _, authed := stub.captured(); authed {
		t.Error("no credentials were configured, so the sender must not authenticate")
	}
}

func TestSMTPConfigValidation(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewSMTP())

	good := map[string]any{"host": "smtp.example.com", "from": "a@example.com", "to": "b@example.com"}
	if err := reg.ValidateConfig("smtp", good); err != nil {
		t.Errorf("ValidateConfig = %v, want nil", err)
	}

	bad := []map[string]any{
		{"from": "a@example.com", "to": "b@example.com"},             // no host
		{"host": "h", "to": "b@example.com"},                         // no from
		{"host": "h", "from": "a@example.com"},                       // no recipient
		{"host": "h", "from": "a@example.com", "to": ", ,"},          // only separators
		{"host": "h", "from": "a@", "to": "b@", "port": "99999"},     // port out of range
		{"host": "h", "from": "a@", "to": "b@", "port": "not a num"}, // unparseable port
	}
	for _, cfg := range bad {
		if err := reg.ValidateConfig("smtp", cfg); err == nil {
			t.Errorf("ValidateConfig(%v) should have failed", cfg)
		}
	}
}

// The UI's channel editor offers the types this registry reports, so the set has to
// stay in step with internal/webui/assets/js/views/notifications.js
// (jstest/channels.test.mjs pins the other end).
func TestRegistryShipsTheExpectedChannelTypes(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewNtfy(nil))
	reg.Register(NewWebhook(nil))
	reg.Register(NewGotify(nil))
	reg.Register(NewTelegram(nil))
	reg.Register(NewDiscord(nil))
	reg.Register(NewSMTP())

	got := append([]string(nil), reg.Types()...)
	sort.Strings(got)
	want := []string{"discord", "gotify", "ntfy", "smtp", "telegram", "webhook"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("registered types = %v, want %v\n"+
			"update CHANNEL_TYPES in internal/webui/assets/js/views/notifications.js to match", got, want)
	}
}

// An unregistered type must be rejected rather than silently accepted and then
// failing on every event.
func TestRegistryRejectsUnknownTypes(t *testing.T) {
	reg := NewRegistry()
	if _, ok := reg.Get("carrier-pigeon"); ok {
		t.Error("Get should not resolve an unregistered type")
	}
	if err := reg.ValidateConfig("carrier-pigeon", map[string]any{}); err == nil {
		t.Error("ValidateConfig should reject an unregistered type")
	}
}

// The subject carries the device model, so it has to be encoded or a non-ASCII
// model arrives as mojibake.
func TestEmailSubjectIsEncoded(t *testing.T) {
	msg := buildEmail(
		map[string]any{"from": "a@example.com", "to": "b@example.com"},
		Event{Type: EventHealthChange, DeviceName: "bööt"},
	)
	if !strings.Contains(msg, "=?utf-8?") {
		t.Errorf("subject should be RFC 2047 encoded:\n%s", msg)
	}
}
