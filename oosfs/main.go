// Package main provides oosfs — a filesystem MCP server tailored for
// LLM-driven development work on the onisin OS monorepo.
//
// oosfs replaces @modelcontextprotocol/server-filesystem but is designed for
// a trusted, single-user context. It favors structured JSON output, content
// search, and project awareness over defensive limits.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/server"

	"onisin.com/oosfs/internal/roots"
	"onisin.com/oosfs/internal/tools"
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	flagHTTP := flag.String("http", "", "optional: run as HTTP server on this address (e.g. :8765) instead of stdio")
	flagLog := flag.String("log", "stderr", "log destination: 'stderr', 'none', or a file path")
	flagQuiet := flag.Bool("quiet", false, "suppress info logs (only warnings and errors)")
	flagVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = usage
	flag.Parse()

	if *flagVersion {
		fmt.Println(version)
		return
	}

	allowed := flag.Args()
	if len(allowed) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one allowed directory is required")
		usage()
		os.Exit(2)
	}

	logger, closeLog, err := setupLogger(*flagLog, *flagQuiet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: setup logger: %v\n", err)
		os.Exit(1)
	}
	defer closeLog()

	registry, err := roots.New(allowed, logger)
	if err != nil {
		logger.Error("resolve allowed directories", "err", err)
		os.Exit(1)
	}
	for _, r := range registry.All() {
		logger.Info("allowed root", "path", r)
	}
	if v := os.Getenv("OOSFS_TRUSTED"); v == "1" || v == "true" || v == "yes" {
		logger.Info("trusted mode enabled — all tools advertised as read-only")
	}

	mcpServer := server.NewMCPServer(
		"oosfs",
		version,
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	tools.RegisterAll(mcpServer, registry, logger)

	if *flagHTTP != "" {
		runHTTP(mcpServer, *flagHTTP, logger)
		return
	}
	runStdio(mcpServer, logger)
}

// runStdio starts the server in stdio mode — the default for Claude Desktop.
func runStdio(s *server.MCPServer, logger *slog.Logger) {
	logger.Info("starting oosfs", "mode", "stdio", "version", version)
	if err := server.ServeStdio(s); err != nil {
		logger.Error("stdio server failed", "err", err)
		os.Exit(1)
	}
}

// runHTTP starts the server in streamable-HTTP mode. Useful when oosfs should
// be reached by several clients at once or when debugging with curl.
func runHTTP(s *server.MCPServer, addr string, logger *slog.Logger) {
	logger.Info("starting oosfs", "mode", "http", "addr", addr, "version", version)
	httpServer := server.NewStreamableHTTPServer(s)
	if err := httpServer.Start(addr); err != nil {
		logger.Error("http server failed", "err", err)
		os.Exit(1)
	}
}

// setupLogger builds a structured JSON logger writing to the chosen destination.
// Access logs benefit from structured output because they are easy to grep
// after a long session.
func setupLogger(dest string, quiet bool) (*slog.Logger, func(), error) {
	level := slog.LevelInfo
	if quiet {
		level = slog.LevelWarn
	}
	opts := &slog.HandlerOptions{Level: level}

	switch strings.ToLower(dest) {
	case "none":
		return slog.New(slog.NewJSONHandler(discardWriter{}, opts)), func() {}, nil
	case "stderr", "":
		return slog.New(slog.NewJSONHandler(os.Stderr, opts)), func() {}, nil
	default:
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file: %w", err)
		}
		return slog.New(slog.NewJSONHandler(f, opts)), func() { _ = f.Close() }, nil
	}
}

// discardWriter silently swallows all writes. Used for --log=none.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func usage() {
	fmt.Fprintf(os.Stderr, `oosfs — filesystem MCP server for onisin OS

Usage:
  oosfs [flags] <allowed-dir> [allowed-dir ...]

Flags:
  --http <addr>    run as HTTP server on this address instead of stdio
  --log <dest>     log to 'stderr' (default), 'none', or a file path
  --quiet          suppress info logs
  --version        print version and exit

Example:
  oosfs /Users/frank/repro/onisin
  oosfs --log /tmp/oosfs.log /Users/frank/repro
`)
}
