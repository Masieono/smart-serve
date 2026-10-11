// Command smartserve is the Smart-Serve entry point.
//
// At this stage it exposes a `scan` subcommand that discovers devices and prints
// a JSON report. The HTTP server, scheduler, and notifications are added next
// (see docs/SCOPE.md).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/masieono/smart-serve/internal/scan"
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
