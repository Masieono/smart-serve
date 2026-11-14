// Package config reads Smart-Serve's operational configuration from the
// environment. Per ADR-009 only bootstrap/operational settings live here; every
// tunable threshold lives in the database and is edited through the UI.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the process configuration.
type Config struct {
	Addr            string        // SMARTSERVE_ADDR
	DBPath          string        // SMARTSERVE_DB
	BasePath        string        // SMARTSERVE_BASE_PATH (subpath hosting, ADR-015)
	CommandTimeout  time.Duration // SMARTSERVE_COMMAND_TIMEOUT (seconds)
	MissedRunPolicy string        // SMARTSERVE_MISSED_RUN_POLICY: skip | run_once
	LogLevel        string        // SMARTSERVE_LOG_LEVEL
	Version         string        // build version (set by the caller)
}

// Load reads configuration from the environment, applying documented defaults.
func Load() Config {
	return Config{
		Addr:            env("SMARTSERVE_ADDR", ":8080"),
		DBPath:          env("SMARTSERVE_DB", "/data/smartserve.db"),
		BasePath:        normalizeBasePath(env("SMARTSERVE_BASE_PATH", "/")),
		CommandTimeout:  time.Duration(envInt("SMARTSERVE_COMMAND_TIMEOUT", 15)) * time.Second,
		MissedRunPolicy: env("SMARTSERVE_MISSED_RUN_POLICY", "skip"),
		LogLevel:        env("SMARTSERVE_LOG_LEVEL", "info"),
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	// strconv rather than a hand-rolled digit loop: the loop this replaced added digits
	// without checking, so a value too large to represent wrapped into some other number.
	// It wrapped into a *positive* one, which is the worst version — every caller checks
	// `> 0` before using the value, so a 20-digit timeout arrived as 2190664h and was
	// accepted. strconv reports the overflow, and a setting that cannot be represented
	// falls back to the default like any other unusable value.
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// normalizeBasePath returns "" for the root, otherwise a leading-slash,
// no-trailing-slash prefix (e.g. "/smartserve").
func normalizeBasePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}
