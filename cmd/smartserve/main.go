// Command smartserve is the Smart-Serve entry point.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/masieono/smart-serve/internal/config"
	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/httpapi"
	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/notify"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/sched"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
	"github.com/masieono/smart-serve/internal/webui"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// It must stay a var, not a const, or the linker has nothing to overwrite. The fallback
// is what a plain `go build`, `go run` or `go test` produces, so the binary is always
// honest about being unstamped rather than claiming a release it is not.
var version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "scan":
		if err := runScan(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "serve":
		if err := runServe(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("smart-serve", version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `smart-serve — S.M.A.R.T. reporting for every device in lsblk

Usage:
  smart-serve serve    run the HTTP API (and, soon, the web UI)
  smart-serve scan     discover devices and print a S.M.A.R.T. report as JSON
  smart-serve version  print the version
`)
}

func runScan() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	devices, err := scan.New().Run(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(devices)
}

// runServe starts the HTTP API. Access control is delegated to a reverse proxy
// (ADR-016); the server itself is unauthenticated by design.
func runServe() error {
	cfg := config.Load()
	cfg.Version = version

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	hub := events.NewHub()

	// The tunables live in the database and are read per operation, so a change made
	// in the UI applies to the very next scan rather than at the next restart
	// (ADR-009). The values below are only the fallbacks.
	settings := func(ctx context.Context) map[string]string {
		values, err := st.GetSettings(ctx)
		if err != nil {
			return nil
		}
		return values
	}

	scanner := &scan.Scanner{
		Timeout:    cfg.CommandTimeout,
		Thresholds: interp.DefaultThresholds(),
		TimeoutFunc: func(ctx context.Context) time.Duration {
			return secondsSetting(settings(ctx), "command_timeout_seconds")
		},
		ThresholdsFunc: func(ctx context.Context) interp.Thresholds {
			return interp.FromSettings(settings(ctx))
		},
	}
	manager := scan.NewManager(scanner, st, hub)

	// One device-access probe at startup. The three ways a container blocks S.M.A.R.T.
	// all look identical from inside, so without this the first symptom is an empty
	// dashboard and a message claiming the drive has no S.M.A.R.T. — which is wrong,
	// and sends the reader looking in the wrong place. One command, once.
	if probe, perr := smart.ProbeDeviceAccess(ctx, cfg.CommandTimeout); perr != nil {
		logger.Warn("device access probe could not run", "error", perr)
	} else {
		switch {
		case probe.Devices == 0:
			logger.Warn("no block devices visible — is /dev mounted into the container?")
		case probe.Cause.Blocked():
			logger.Warn("S.M.A.R.T. reads are blocked, not unsupported",
				"device", probe.Path, "cause", string(probe.Cause),
				"detail", probe.Detail, "hint", probe.Cause.Hint())
		case probe.Cause != smart.CauseNone:
			logger.Info("device access probe inconclusive",
				"device", probe.Path, "cause", string(probe.Cause), "detail", probe.Detail)
		default:
			logger.Info("device access ok", "devices", probe.Devices, "probed", probe.Path)
		}
	}

	// Notification delivery (docs/NOTIFICATIONS.md). Senders are stateless and
	// registered by channel type, so adding a provider never touches the scan
	// pipeline. The dispatcher is also handed to the API, which uses it to
	// validate channel configs and to send test events.
	registry := notify.NewRegistry()
	registry.Register(notify.NewNtfy(nil))
	registry.Register(notify.NewWebhook(nil))
	registry.Register(notify.NewGotify(nil))
	registry.Register(notify.NewTelegram(nil))
	registry.Register(notify.NewDiscord(nil))
	registry.Register(notify.NewSMTP())
	dispatcher := notify.NewDispatcher(st, registry, notify.DispatcherOptions{Logger: logger})
	manager.OnPostScan(notify.NewNotifier(st, dispatcher, cfg.Version, logger).OnScan)

	// The scheduler owns scheduled scans (ADR-004). It sleeps to the next due
	// schedule rather than ticking, so it costs nothing while idle. The timezone and
	// missed-run policy are read from settings, with the environment as the seed.
	scheduler := sched.New(st, manager, hub, sched.Options{
		MissedRunPolicy: cfg.MissedRunPolicy,
		Logger:          logger,
	})
	go scheduler.Run(ctx)

	// The UI theme is read from settings on each shell request, so changing it in
	// the UI is reflected on the next load without a restart (docs/UI.md §5).
	ui, err := webui.New(func(ctx context.Context) webui.Theme {
		settings, serr := st.GetSettings(ctx)
		if serr != nil {
			return webui.DefaultTheme
		}
		return webui.Theme{
			Name:   settings["theme"],
			Base:   settings["theme_base"],
			Accent: settings["theme_accent"],
			Tint:   settings["theme_surface_tint"],
		}
	}, webui.Options{BasePath: cfg.BasePath, Version: cfg.Version})
	if err != nil {
		return err
	}

	api := httpapi.New(st, manager, hub, httpapi.Options{
		BasePath:  cfg.BasePath,
		Version:   cfg.Version,
		Logger:    logger,
		UI:        ui,
		Scheduler: scheduler,
		Notifier:  dispatcher,
		// The self-test endpoints call smartctl directly, so they resolve the same timeout the
		// scans do rather than falling back to the default and ignoring the setting.
		CommandTimeout: scanner.CommandTimeout,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("smart-serve listening",
			"addr", cfg.Addr,
			"db", cfg.DBPath,
			"base_path", firstNonEmpty(cfg.BasePath, "/"),
			"version", cfg.Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func logLevel(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// secondsSetting reads a positive whole-seconds setting, returning 0 when it is
// absent or unparseable so the caller keeps its own default.
func secondsSetting(settings map[string]string, key string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(settings[key]))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
