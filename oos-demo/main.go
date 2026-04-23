package main

// main.go — OOS Demo — starts all services natively for a local demo run.
//
// Typical flow after cloning the repository and running `make compile`:
//
//   ./dist/oos-demo_macos --seed-internal   Install the internal oos schema
//   ./dist/oos-demo_macos --seed-demo       Install public schema + demo data
//   ./dist/oos-demo_macos                   Start all services
//   ./dist/oos-demo_macos --stop            Stop all services
//
// Both seed steps write directly to PostgreSQL and do not need oosp
// to be running. Running --seed-demo before the services are started
// is in fact preferred: it ensures the event_mappings table is populated
// before oosp boots, so oosp's event listener picks up the mappings
// immediately instead of idling on an empty table.
//
// Binaries are resolved from ./dist relative to the current working
// directory — the demo is meant to be started from the repository root.
//
// Ollama must be started separately by the user — oos-demo does not manage it.

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pterm/pterm"
	"onisin.com/oos-demo/seed"
)

func main() {
	seedInternalFlag := flag.Bool("seed-internal", false, "Install the internal oos schema (required once before first start)")
	seedDemoFlag     := flag.Bool("seed-demo", false, "Install the public schema and demo data (run after the services are up)")
	stopFlag         := flag.Bool("stop", false, "Stop all services")
	flag.Parse()

	printBanner()

	cfg, err := LoadConfig()
	if err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}

	pterm.Info.Printf("Binaries: %s\n", cfg.BinDir())
	pterm.Info.Printf("Data:     %s\n", cfg.DataDir())
	pterm.Info.Printf("Logs:     %s\n", cfg.LogDir())
	fmt.Println()

	// --seed-internal
	if *seedInternalFlag {
		pterm.DefaultSection.Println("Installing internal schema")

		opts := seed.Options{
			DSN:      cfg.PostgresDSN(),
			AdminDSN: cfg.PostgresAdminDSN(),
			Database: cfg.PostgreSQL.Database,
			AppUsers: cfg.PostgreSQL.AppUsers,
		}
		if err := seed.Internal(opts); err != nil {
			pterm.Error.Println(err)
			os.Exit(1)
		}
		pterm.Success.Println("Internal schema installed")
		pterm.Info.Println("Next step: --seed-demo")
		return
	}

	// --seed-demo
	if *seedDemoFlag {
		pterm.DefaultSection.Println("Installing public schema and demo data")

		opts := seed.Options{
			DSN:      cfg.PostgresDSN(),
			AdminDSN: cfg.PostgresAdminDSN(),
			Database: cfg.PostgreSQL.Database,
			AppUsers: cfg.PostgreSQL.AppUsers,
		}
		if err := seed.Demo(opts); err != nil {
			pterm.Error.Println(err)
			os.Exit(1)
		}
		pterm.Success.Println("Public schema and demo data installed")
		pterm.Info.Println("Next step: ./dist/oos-demo_<platform>   (no flag)")
		return
	}

	// Ensure directories exist
	if err := cfg.EnsureDirs(); err != nil {
		pterm.Error.Println("Creating directories:", err)
		os.Exit(1)
	}

	mgr := NewManager(cfg)

	// --stop
	if *stopFlag {
		mgr.StopAll()
		return
	}

	// Normal start
	pterm.DefaultSection.Println("Starting services")
	if err := mgr.StartAll(); err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}

	pterm.Success.Println("OOS running — press Ctrl+C to stop")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	fmt.Println()
	pterm.DefaultSection.Println("Stopping all services...")
	mgr.StopAll()
	pterm.Success.Println("Done")
}

func printBanner() {
	pterm.DefaultBigText.WithLetters(
		pterm.NewLettersFromStringWithStyle("o", pterm.NewStyle(pterm.FgCyan)),
		pterm.NewLettersFromStringWithStyle("OS", pterm.NewStyle(pterm.FgWhite)),
	).Render()
	pterm.DefaultHeader.
		WithBackgroundStyle(pterm.NewStyle(pterm.BgDarkGray)).
		WithTextStyle(pterm.NewStyle(pterm.FgLightWhite)).
		Println("OOS Demo")
	fmt.Println()
}
