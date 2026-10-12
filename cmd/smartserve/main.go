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
	"syscall"
	"time"

	"github.com/masieono/smart-serve/internal/config"
	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/httpapi"
	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/store"
	"github.com/masieono/smart-serve/internal/webui"
)

const version = "0.0.0-dev"

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
	scanner := &scan.Scanner{Timeout: cfg.CommandTimeout, Thresholds: interp.DefaultThresholds()}
	manager := scan.NewManager(scanner, st, hub)

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
		BasePath: cfg.BasePath,
		Version:  cfg.Version,
		Logger:   logger,
		UI:       ui,
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
