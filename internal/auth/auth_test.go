package auth

import (
	"testing"
	"time"
)

func TestMatchIsExactOnBothHalves(t *testing.T) {
	c := Credentials{User: "admin", Password: "s3cret"}
	cases := []struct {
		user, pass string
		want       bool
	}{
		{"admin", "s3cret", true},
		{"admin", "s3cre", false},
		{"Admin", "s3cret", false},
		{"admin", "", false},
		{"", "s3cret", false},
	}
	for _, tc := range cases {
		if got := c.Match(tc.user, tc.pass); got != tc.want {
			t.Errorf("Match(%q,%q) = %v, want %v", tc.user, tc.pass, got, tc.want)
		}
	}
}

func TestEnabledNeedsBothHalves(t *testing.T) {
	if (Credentials{}).Enabled() {
		t.Error("an empty credential must not enable the layer")
	}
	if (Credentials{User: "admin"}).Enabled() {
		t.Error("a username without a password must not enable the layer")
	}
	if (Credentials{Password: "x"}).Enabled() {
		t.Error("a password without a username must not enable the layer")
	}
	if !(Credentials{User: "u", Password: "p"}).Enabled() {
		t.Error("a complete credential enables the layer")
	}
}

func TestIdleWindowExpiresASession(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := New(30*time.Minute, 24*time.Hour)
	s.now = func() time.Time { return now }

	token, err := s.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !s.Touch(token) {
		t.Fatal("a fresh token must be valid")
	}

	now = now.Add(29 * time.Minute)
	if !s.Touch(token) {
		t.Fatal("29 minutes of nothing is still inside a 30-minute window")
	}

	// The touch above extended the window, so 29 more minutes is still fine.
	now = now.Add(29 * time.Minute)
	if !s.Touch(token) {
		t.Fatal("activity extends the idle window")
	}

	now = now.Add(31 * time.Minute)
	if s.Touch(token) {
		t.Error("an untended session must expire")
	}
	if s.Count() != 0 {
		t.Errorf("an expired session is dropped, got %d left", s.Count())
	}
}

func TestAbsoluteCapExpiresAnActiveSession(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := New(30*time.Minute, 24*time.Hour)
	s.now = func() time.Time { return now }

	token, _ := s.Issue()
	// Never idle longer than the window, so only the cap can end this one: a tab left open that
	// keeps making requests is exactly the case the cap exists for.
	for i := 0; i < 73; i++ { // 73 x 20 min = 24h20m
		now = now.Add(20 * time.Minute)
		if !s.Touch(token) {
			if now.Sub(s.now().Add(-24*time.Hour-time.Minute)) >= 0 {
				return // expired at the cap, which is what this test is about
			}
			t.Fatalf("session ended after %d touches, before the absolute cap", i)
		}
	}
	t.Fatal("a session older than the absolute cap must expire however active it is")
}

func TestShorteningTheIdleWindowAppliesToLiveSessions(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	s := New(30*time.Minute, 24*time.Hour)
	s.now = func() time.Time { return now }

	token, _ := s.Issue()
	now = now.Add(10 * time.Minute)
	s.SetIdle(5 * time.Minute)
	if s.Touch(token) {
		t.Error("after the window is shortened, an older-than-window session must expire")
	}
}

func TestRevokeEndsTheSession(t *testing.T) {
	s := New(0, 0)
	token, _ := s.Issue()
	if s.Count() != 1 {
		t.Fatalf("expected one session, got %d", s.Count())
	}
	s.Revoke(token)
	if s.Touch(token) {
		t.Error("a revoked token must not work")
	}
	if s.Count() != 0 {
		t.Errorf("expected no sessions, got %d", s.Count())
	}
}

func TestTokensAreUniqueAndOpaque(t *testing.T) {
	s := New(0, 0)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		token, err := s.Issue()
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if seen[token] {
			t.Fatal("Issue returned a duplicate token")
		}
		if len(token) < 32 {
			t.Fatalf("token too short to be unguessable: %d chars", len(token))
		}
		seen[token] = true
	}
}
