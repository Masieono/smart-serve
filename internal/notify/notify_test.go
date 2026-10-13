package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/store"
)

// --- test doubles -------------------------------------------------------------

type loggedEntry struct {
	channelID int64
	eventType string
	eventKey  string
	at        time.Time
	status    string
	detail    string
}

// fakeStore is an in-memory Store that records what the dispatcher did.
type fakeStore struct {
	mu          sync.Mutex
	channels    []store.Channel
	logged      []loggedEntry
	last        map[string]time.Time
	settings    map[string]string
	settingsErr error
}

func newFakeStore(channels ...store.Channel) *fakeStore {
	return &fakeStore{channels: channels, last: map[string]time.Time{}}
}

func (f *fakeStore) ListChannels(context.Context) ([]store.Channel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.channels, nil
}

func (f *fakeStore) GetSettings(context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.settingsErr != nil {
		return nil, f.settingsErr
	}
	if f.settings == nil {
		return map[string]string{}, nil
	}
	return f.settings, nil
}

func (f *fakeStore) LogNotification(_ context.Context, channelID int64, eventType, eventKey string, at time.Time, status, detail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logged = append(f.logged, loggedEntry{channelID, eventType, eventKey, at, status, detail})
	if status == string(OutcomeSent) {
		f.last[squelchKey(channelID, eventKey)] = at
	}
	return nil
}

func (f *fakeStore) LastNotificationAt(_ context.Context, channelID int64, eventKey string) (*time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if at, ok := f.last[squelchKey(channelID, eventKey)]; ok {
		return &at, nil
	}
	return nil, nil
}

func (f *fakeStore) entries() []loggedEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]loggedEntry(nil), f.logged...)
}

func (f *fakeStore) statuses() []string {
	entries := f.entries()
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.status)
	}
	return out
}

func squelchKey(channelID int64, eventKey string) string {
	return strconv.FormatInt(channelID, 10) + "/" + eventKey
}

// recordingSender counts deliveries instead of making network calls. It can be
// told to fail for specific named configs, which lets a test prove one broken
// channel does not stop its neighbours.
type recordingSender struct {
	typ       string
	failNames map[string]bool

	mu    sync.Mutex
	sent  []Event
	names []string // every attempted delivery, in order
}

func (s *recordingSender) Type() string { return s.typ }

func (s *recordingSender) Send(_ context.Context, cfg map[string]any, ev Event) error {
	name, _ := cfg["name"].(string)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names = append(s.names, name)
	if s.failNames[name] {
		return errors.New("delivery failed for " + name)
	}
	s.sent = append(s.sent, ev)
	return nil
}

func (s *recordingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func (s *recordingSender) attempts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.names...)
}

// onlyLogged returns the single delivery-log entry, for tests that expect exactly one.
func (f *fakeStore) onlyLogged() (loggedEntry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.logged) != 1 {
		return loggedEntry{}, false
	}
	return f.logged[0], true
}

func testDispatcher(st Store, sender Sender, opts DispatcherOptions) *Dispatcher {
	reg := NewRegistry()
	if sender != nil {
		reg.Register(sender)
	}
	if opts.Logger == nil {
		opts.Logger = discardLogger()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return NewDispatcher(st, reg, opts)
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// --- dispatcher ---------------------------------------------------------------

func TestDispatchHonoursEnabledAndSubscriptions(t *testing.T) {
	st := newFakeStore(
		store.Channel{ID: 1, Type: "rec", Label: "all", Enabled: true, Events: "[]"},
		store.Channel{ID: 2, Type: "rec", Label: "disappear only", Enabled: true, Events: `["device_disappeared"]`},
		store.Channel{ID: 3, Type: "rec", Label: "off", Enabled: false, Events: "[]"},
	)
	sender := &recordingSender{typ: "rec"}
	d := testDispatcher(st, sender, DispatcherOptions{})

	d.Dispatch(context.Background(), []Event{{Type: EventHealthChange, Serial: "S1"}})

	if sender.count() != 1 {
		t.Errorf("deliveries = %d, want 1 (only the subscribed, enabled channel)", sender.count())
	}
	entries := st.entries()
	if len(entries) != 1 || entries[0].channelID != 1 || entries[0].status != string(OutcomeSent) {
		t.Errorf("log = %+v, want a single sent entry for channel 1", entries)
	}
}

func TestDispatchRecordsAFailureWithoutStopping(t *testing.T) {
	st := newFakeStore(
		store.Channel{ID: 1, Type: "rec", Enabled: true, Config: `{"name":"broken"}`, Events: "[]"},
		store.Channel{ID: 2, Type: "rec", Enabled: true, Config: `{"name":"working"}`, Events: "[]"},
	)
	sender := &recordingSender{typ: "rec", failNames: map[string]bool{"broken": true}}
	d := testDispatcher(st, sender, DispatcherOptions{})

	d.Dispatch(context.Background(), []Event{{Type: EventHealthChange, Serial: "S1"}})

	if sender.count() != 1 {
		t.Errorf("deliveries = %d, want the working channel to still receive it", sender.count())
	}
	if got := st.statuses(); len(got) != 2 || got[0] != string(OutcomeFailed) || got[1] != string(OutcomeSent) {
		t.Errorf("statuses = %v, want [failed sent]", got)
	}
}

func TestDispatchSkipsUnknownChannelTypes(t *testing.T) {
	st := newFakeStore(store.Channel{ID: 1, Type: "carrier-pigeon", Enabled: true, Events: "[]"})
	sender := &recordingSender{typ: "rec"}
	d := testDispatcher(st, sender, DispatcherOptions{})

	d.Dispatch(context.Background(), []Event{{Type: EventHealthChange, Serial: "S1"}})

	if sender.count() != 0 {
		t.Errorf("deliveries = %d, want 0 for an unsupported channel type", sender.count())
	}
	if len(st.entries()) != 0 {
		t.Errorf("log = %+v, want no entries — nothing was attempted", st.entries())
	}
}

func TestDispatchWithNoEventsDoesNothing(t *testing.T) {
	st := newFakeStore(store.Channel{ID: 1, Type: "rec", Enabled: true})
	sender := &recordingSender{typ: "rec"}
	d := testDispatcher(st, sender, DispatcherOptions{})

	d.Dispatch(context.Background(), nil)

	if sender.count() != 0 || len(st.entries()) != 0 {
		t.Errorf("deliveries = %d, log = %+v; want nothing", sender.count(), st.entries())
	}
}

func TestRegistryTypesAreSorted(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewWebhook(nil))
	reg.Register(NewNtfy(nil))

	types := reg.Types()
	if len(types) != 2 || types[0] != "ntfy" || types[1] != "webhook" {
		t.Errorf("Types() = %v, want [ntfy webhook]", types)
	}
}

// A condition that persists is squelched for the window; a real transition is
// not, because a drive that recovers and degrades again is two separate events.
func TestSquelchWindowDependsOnTheEventType(t *testing.T) {
	window := time.Hour
	if got := squelchWindow(Event{Type: EventTemperatureHigh}, window); got != window {
		t.Errorf("temperature window = %v, want %v", got, window)
	}
	for _, typ := range []string{EventHealthChange, EventDeviceDisappeared, EventDeviceAppeared, EventScanFailure} {
		if got := squelchWindow(Event{Type: typ}, window); got != 0 {
			t.Errorf("%s window = %v, want 0 (never squelched)", typ, got)
		}
	}
}

// The squelch window is a setting the user can tune, so it has to be read.
func TestConfiguredSquelchHonoursTheSetting(t *testing.T) {
	st := newFakeStore()
	st.settings = map[string]string{"squelch_seconds": "120"}
	d := testDispatcher(st, nil, DispatcherOptions{Squelch: time.Hour})

	if got := d.newPass(context.Background()).squelch; got != 2*time.Minute {
		t.Errorf("squelch = %v, want 2m from the setting", got)
	}
}

func TestConfiguredSquelchFallsBackToTheDefault(t *testing.T) {
	d := testDispatcher(newFakeStore(), nil, DispatcherOptions{Squelch: 45 * time.Minute})

	if got := d.newPass(context.Background()).squelch; got != 45*time.Minute {
		t.Errorf("squelch = %v, want the configured default", got)
	}
}

// A settings read that fails must not stop alerts: the pass falls back to the
// configured defaults rather than delivering nothing.
func TestPassSurvivesAnUnreadableSettingsTable(t *testing.T) {
	st := newFakeStore()
	st.settingsErr = errors.New("database is locked")
	d := testDispatcher(st, nil, DispatcherOptions{Squelch: 45 * time.Minute})

	p := d.newPass(context.Background())
	if p.squelch != 45*time.Minute {
		t.Errorf("squelch = %v, want the configured default", p.squelch)
	}
	if len(p.windows) != 0 {
		t.Errorf("windows = %v, want none", p.windows)
	}
}

// End to end: a 60 s setting must let a repeat through where the 1 h default would
// have swallowed it.
func TestDispatchSquelchesUsingTheConfiguredWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	st := newFakeStore(store.Channel{ID: 1, Type: "rec", Enabled: true, Events: "[]"})
	st.settings = map[string]string{"squelch_seconds": "60"}
	sender := &recordingSender{typ: "rec"}
	hot := Event{Type: EventTemperatureHigh, Serial: "S1"}

	testDispatcher(st, sender, DispatcherOptions{
		Now: func() time.Time { return now }, Squelch: time.Hour,
	}).Dispatch(context.Background(), []Event{hot})

	testDispatcher(st, sender, DispatcherOptions{
		Now: func() time.Time { return now.Add(30 * time.Minute) }, Squelch: time.Hour,
	}).Dispatch(context.Background(), []Event{hot})

	if sender.count() != 2 {
		t.Errorf("deliveries = %d, want 2 — the configured 60s window had elapsed", sender.count())
	}
}

func TestSubscribed(t *testing.T) {
	cases := []struct {
		events string
		typ    string
		want   bool
	}{
		{"[]", EventHealthChange, true},                          // empty means everything
		{"", EventHealthChange, true},                            // absent means everything
		{"not json", EventHealthChange, true},                    // garbage must not silence alerts
		{`["device_disappeared"]`, EventDeviceDisappeared, true}, // listed
		{`["device_disappeared"]`, EventHealthChange, false},     // not listed
		{`["*"]`, EventHealthChange, true},                       // wildcard
	}
	for _, c := range cases {
		if got := subscribed(c.events, c.typ); got != c.want {
			t.Errorf("subscribed(%q, %q) = %v, want %v", c.events, c.typ, got, c.want)
		}
	}
}

func TestDecodeConfigToleratesGarbage(t *testing.T) {
	if cfg := decodeConfig(""); len(cfg) != 0 {
		t.Errorf("decodeConfig(\"\") = %v, want empty", cfg)
	}
	if cfg := decodeConfig("not json"); len(cfg) != 0 {
		t.Errorf("decodeConfig(garbage) = %v, want empty", cfg)
	}
	cfg := decodeConfig(`{"topic":"alerts"}`)
	if cfg["topic"] != "alerts" {
		t.Errorf("decodeConfig = %v", cfg)
	}
}
