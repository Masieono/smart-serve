package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// envKeys is every variable Load reads. Tests clear all of them so a developer's own
// exported SMARTSERVE_* cannot decide what a test asserts.
var envKeys = []string{
	"SMARTSERVE_ADDR",
	"SMARTSERVE_DB",
	"SMARTSERVE_BASE_PATH",
	"SMARTSERVE_COMMAND_TIMEOUT",
	"SMARTSERVE_MISSED_RUN_POLICY",
	"SMARTSERVE_LOG_LEVEL",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range envKeys {
		t.Setenv(k, "")
	}
}

// TestDefaults pins the values a container gets with no environment at all, which is how
// most people run this, and the only configuration the README ever shows.
func TestDefaults(t *testing.T) {
	clearEnv(t)
	cfg := Load()

	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.DBPath != "/data/smartserve.db" {
		t.Errorf("DBPath = %q, want /data/smartserve.db", cfg.DBPath)
	}
	if cfg.BasePath != "" {
		t.Errorf("BasePath = %q, want empty for the root", cfg.BasePath)
	}
	if cfg.CommandTimeout != 15*time.Second {
		t.Errorf("CommandTimeout = %v, want 15s", cfg.CommandTimeout)
	}
	if cfg.MissedRunPolicy != "skip" {
		t.Errorf("MissedRunPolicy = %q, want skip", cfg.MissedRunPolicy)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
}

func TestEnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("SMARTSERVE_ADDR", ":9000")
	t.Setenv("SMARTSERVE_DB", "/tmp/x.db")
	t.Setenv("SMARTSERVE_BASE_PATH", "/smartserve")
	t.Setenv("SMARTSERVE_COMMAND_TIMEOUT", "30")
	t.Setenv("SMARTSERVE_MISSED_RUN_POLICY", "run_once")
	t.Setenv("SMARTSERVE_LOG_LEVEL", "debug")

	cfg := Load()
	if cfg.Addr != ":9000" || cfg.DBPath != "/tmp/x.db" || cfg.BasePath != "/smartserve" {
		t.Errorf("string settings not honoured: %+v", cfg)
	}
	if cfg.CommandTimeout != 30*time.Second {
		t.Errorf("CommandTimeout = %v, want 30s", cfg.CommandTimeout)
	}
	if cfg.MissedRunPolicy != "run_once" || cfg.LogLevel != "debug" {
		t.Errorf("policy/level not honoured: %+v", cfg)
	}
}

// TestNormalizeBasePath is the ADR-015 subpath-hosting path, and the one with the least
// obvious failure: a prefix that keeps a trailing slash produces routes that never match,
// and the symptom is a blank page behind a proxy rather than an error.
func TestNormalizeBasePath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},                        // unset: root
		{"/", ""},                       // explicitly root
		{"  ", ""},                      // whitespace is not a path
		{"smartserve", "/smartserve"},   // a bare word gets its leading slash
		{"/smartserve", "/smartserve"},  // already right
		{"/smartserve/", "/smartserve"}, // trailing slash is removed
		{"  /smartserve/  ", "/smartserve"},
		{"/a/b", "/a/b"}, // nesting is allowed
		{"//", ""},       // TrimRight takes both slashes
		{"/smartserve//", "/smartserve"},
	} {
		if got := normalizeBasePath(tc.in); got != tc.want {
			t.Errorf("normalizeBasePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCommandTimeoutParsing covers how the command timeout is read: anything that is not a
// positive whole number falls back to the default rather than becoming zero, because a zero
// timeout means "no limit" to exec and a drive that has stopped answering would then hang
// the request for as long as the kernel allows. The last case is the one that was wrong, a
// value too large to represent wrapped into a positive duration, which the caller's `> 0`
// check then accepted.
func TestCommandTimeoutParsing(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"", 15 * time.Second},
		{"0", 15 * time.Second},
		{"-5", 15 * time.Second},
		{"abc", 15 * time.Second},
		{"12s", 15 * time.Second},
		{" 12 ", 15 * time.Second},
		{"45", 45 * time.Second},
		{"1", time.Second},
		// A value too large to represent must fall back, not wrap. Wrapping a 20-digit
		// number can land on a positive duration that is absurd rather than zero, so the
		// caller would keep it instead of the default.
		{"99999999999999999999", 15 * time.Second},
		// Values that fit an int but overflow the *duration* multiplication (~9.2e9 seconds).
		// This is the case the first fix missed: 18446744074 seconds wrapped to 0.3 s, which a
		// caller cannot tell from a drive that has stopped answering.
		{"9223372037", 15 * time.Second},
		{"18446744074", 15 * time.Second},
	} {
		clearEnv(t)
		t.Setenv("SMARTSERVE_COMMAND_TIMEOUT", tc.in)
		if got := Load().CommandTimeout; got != tc.want {
			t.Errorf("SMARTSERVE_COMMAND_TIMEOUT=%q gave %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAuthIsOffWithNoEnvironment(t *testing.T) {
	t.Setenv("SMARTSERVE_AUTH_USER", "")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD", "")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD_FILE", "")

	c := Load()
	if c.AuthUser != "" || c.AuthPassword != "" {
		t.Fatalf("auth should be unconfigured, got user=%q", c.AuthUser)
	}
	if c.AuthIssue != "" {
		t.Fatalf("no environment is not an error: %q", c.AuthIssue)
	}
}

func TestHalfACredentialIsAnIssue(t *testing.T) {
	t.Setenv("SMARTSERVE_AUTH_PASSWORD_FILE", "")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD", "")
	t.Setenv("SMARTSERVE_AUTH_USER", "admin")
	if c := Load(); c.AuthIssue == "" {
		t.Error("a username with no password must be refused, not read as 'off'")
	}

	t.Setenv("SMARTSERVE_AUTH_USER", "")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD", "s3cret")
	if c := Load(); c.AuthIssue == "" {
		t.Error("a password with no username must be refused")
	}
}

func TestPasswordFileIsReadAndTrimmedOfOneNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("correct horse\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("SMARTSERVE_AUTH_USER", "admin")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD", "")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD_FILE", path)

	c := Load()
	if c.AuthIssue != "" {
		t.Fatalf("unexpected issue: %s", c.AuthIssue)
	}
	if c.AuthPassword != "correct horse" {
		t.Errorf("password = %q, want the file's contents without the trailing newline", c.AuthPassword)
	}
}

func TestInlinePasswordWinsOverTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("SMARTSERVE_AUTH_USER", "admin")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD", "from-env")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD_FILE", path)

	if c := Load(); c.AuthPassword != "from-env" {
		t.Errorf("password = %q, want the inline value", c.AuthPassword)
	}
}

func TestMissingPasswordFileIsAnIssue(t *testing.T) {
	t.Setenv("SMARTSERVE_AUTH_USER", "admin")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD", "")
	t.Setenv("SMARTSERVE_AUTH_PASSWORD_FILE", filepath.Join(t.TempDir(), "nope"))

	if c := Load(); c.AuthIssue == "" {
		t.Error("an unreadable password file must be refused, not treated as no password")
	}
}
