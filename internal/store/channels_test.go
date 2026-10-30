package store

import (
	"context"
	"testing"
)

// The scope is normalised on the way in so that the *empty* list is the only thing that means
// "every group" (`FEATURES.md` B12). A stored `["", " tank "]` would otherwise be a scope that
// matches nothing, and a channel that silently stops speaking is indistinguishable from a quiet
// fleet — the one direction a notification filter must never fail in.
func TestChannelScopeGroupsAreNormalised(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", "[]"},
		{"an empty array means every group", "[]", "[]"},
		{"names are kept in order", `["tank","media"]`, `["tank","media"]`},
		{"whitespace is trimmed", `[" tank ","  media "]`, `["tank","media"]`},
		{"blank names are dropped", `["tank","","   "]`, `["tank"]`},
		{"duplicates collapse, case-insensitively", `["tank","Tank","TANK"]`, `["tank"]`},
		{"the first spelling wins", `["Tank","tank"]`, `["Tank"]`},
		// Garbage reads as "no narrowing" rather than "nothing matches": a value this app cannot
		// parse is a bug, and a bug must not be able to silence a channel.
		{"a non-array is every group", `{"tank":true}`, "[]"},
		{"nonsense is every group", "not json", "[]"},
		{"only blanks is every group", `["",""]`, "[]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeScopeGroups(tc.in); got != tc.want {
				t.Errorf("normalizeScopeGroups(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The round trip, against a real database: the column has to exist, the insert has to name it, and
// the read has to come back with it. `B12` is one migration away from a channel that saves a scope
// and forgets it, which would show every box unticked on a narrowed channel — and the next Save
// would quietly widen it.
func TestChannelScopeGroupsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateChannel(ctx, Channel{
		Type: "ntfy", Label: "tank alerts", Config: `{"topic":"t"}`,
		Enabled: true, Events: `["health_change"]`,
		ScopeGroups: `["tank"," Bench ","tank"]`,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	got, err := s.GetChannel(ctx, id)
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got == nil {
		t.Fatal("GetChannel returned nil for a channel it just created")
	}
	if want := `["tank","Bench"]`; got.ScopeGroups != want {
		t.Errorf("scope_groups = %s, want %s", got.ScopeGroups, want)
	}
	if got.Events != `["health_change"]` {
		t.Errorf("events = %s, want the subscription untouched", got.Events)
	}

	got.ScopeGroups = `["media"]`
	if err := s.UpdateChannel(ctx, *got); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	// Both readers, because they are separate queries and only one of them is on the delivery path.
	listed, err := s.ListChannels(ctx)
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(listed) != 1 || listed[0].ScopeGroups != `["media"]` {
		t.Fatalf("ListChannels = %+v, want one channel scoped to media", listed)
	}

	// And a channel that never had a scope reads as "every group" rather than as a null that a
	// caller has to interpret.
	plain, err := s.CreateChannel(ctx, Channel{Type: "ntfy", Label: "everything", Config: "{}", Enabled: true})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	fresh, err := s.GetChannel(ctx, plain)
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if fresh.ScopeGroups != "[]" {
		t.Errorf("scope_groups = %q for an unscoped channel, want []", fresh.ScopeGroups)
	}
}
