// Package auth is the optional single-user layer: one username and one password from the
// environment, and browser sessions kept in memory.
//
// The design is in ADR-033. Two things about it are deliberate and worth knowing before changing
// anything here:
//
//   - There is no user table and no registration. The credential is the environment, which means
//     "add a user" is not a feature this app can grow by accident.
//   - Sessions live in memory only, so a restart logs everyone out. That is a feature for a
//     single-operator box: it means there is nothing on disk to leak, and no migration to carry.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Credentials is the single configured user. Zero value means authentication is off.
type Credentials struct {
	User     string
	Password string
}

// Enabled reports whether a credential is configured. Both halves are required: a half-configured
// pair is refused at start-up rather than silently disabling the layer.
func (c Credentials) Enabled() bool {
	return c.User != "" && c.Password != ""
}

// Match compares the two halves in constant time, so for equal-length inputs a wrong username costs
// the same as a wrong password. A length mismatch returns before any byte is compared, so the length
// of the configured value is not hidden, which is fine here, because the credential is configuration rather
// than user input.
func (c Credentials) Match(user, password string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(c.User)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(c.Password)) == 1
	return userOK && passOK
}

// Errors a session store can report.
var (
	ErrNoSession = errors.New("no such session")
	ErrExpired   = errors.New("session expired")
)

// DefaultIdle and DefaultMax bound a browser session. The idle window is what "auto-logout after a
// period of inactivity" means; the absolute cap bounds a tab left open, which never goes idle long
// enough to expire on its own.
const (
	DefaultIdle = 30 * time.Minute
	DefaultMax  = 24 * time.Hour
)

type entry struct {
	first time.Time
	last  time.Time
}

// Store holds live sessions. It is safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	sessions map[string]entry

	idle time.Duration
	max  time.Duration
	now  func() time.Time
}

// New builds a store with the given idle and absolute windows. A non-positive value takes the
// default rather than disabling the bound.
func New(idle, max time.Duration) *Store {
	if idle <= 0 {
		idle = DefaultIdle
	}
	if max <= 0 {
		max = DefaultMax
	}
	return &Store{sessions: map[string]entry{}, idle: idle, max: max, now: time.Now}
}

// Idle is the idle window currently in force.
func (s *Store) Idle() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idle
}

// SetIdle changes the idle window, which the settings page does. Sessions already issued are
// re-evaluated against it on their next use, so shortening it takes effect without a restart.
func (s *Store) SetIdle(idle time.Duration) {
	if idle <= 0 {
		idle = DefaultIdle
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idle = idle
}

// Issue creates a session and returns its token.
func (s *Store) Issue() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = entry{first: now, last: now}
	return token, nil
}

// Touch validates a token and extends its idle window. It reports false when the token is unknown or
// either bound has passed, and drops the entry in that case so a dead token is not kept forever.
func (s *Store) Touch(token string) bool {
	if token == "" {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sessions[token]
	if !ok {
		return false
	}
	if now.Sub(e.last) > s.idle || now.Sub(e.first) > s.max {
		delete(s.sessions, token)
		return false
	}
	e.last = now
	s.sessions[token] = e
	return true
}

// Revoke ends a session. Logging out is this and nothing else.
func (s *Store) Revoke(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// Count is the number of live sessions, for tests and for a log line.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}
