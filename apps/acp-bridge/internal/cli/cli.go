// Package cli is the `paca-acp-bridge run ...` entrypoint: flag/env
// parsing and signal handling, matching the old Python bridge's cli.py.
package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Paca-AI/paca/apps/acp-bridge/internal/bridge"
	"github.com/Paca-AI/paca/apps/acp-bridge/internal/runner"
)

const usage = "usage: paca-acp-bridge run --agent-id <id> --token <token> --server <url> " +
	"[--workspace <path>] [--log-level <level>]\n" +
	"       paca-acp-bridge version"

// Main is the CLI's entrypoint, returning the process exit code. version is
// this build's version string (see cmd/paca-acp-bridge/main.go), reported
// by `paca-acp-bridge version` and logged once at startup.
func Main(argv []string, version string) int {
	if len(argv) > 0 && (argv[0] == "version" || argv[0] == "--version") {
		fmt.Println("paca-acp-bridge " + version)
		return 0
	}
	if len(argv) == 0 || argv[0] != "run" {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	agentID := fs.String("agent-id", os.Getenv("PACA_ACP_AGENT_ID"),
		"The ACP agent's id (or PACA_ACP_AGENT_ID)")
	token := fs.String("token", os.Getenv("PACA_ACP_TOKEN"),
		"The agent's local-bridge token, generated in Paca's UI (or PACA_ACP_TOKEN)")
	server := fs.String("server", os.Getenv("PACA_ACP_SERVER"),
		"Your Paca instance's base URL, e.g. https://paca.example.com (or PACA_ACP_SERVER)")
	cwd, _ := os.Getwd()
	workspace := fs.String("workspace", cwd,
		"Directory the ACP server operates on (default: current directory)")
	logLevel := fs.String("log-level", "INFO", "Log level: DEBUG, INFO, WARN, or ERROR")

	if err := fs.Parse(argv[1:]); err != nil {
		return 2
	}

	log := newLogger(*logLevel)

	if *agentID == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "error: --agent-id and --token are required (or PACA_ACP_AGENT_ID / PACA_ACP_TOKEN)")
		return 2
	}
	if *server == "" {
		fmt.Fprintln(os.Stderr,
			"error: --server is required (or PACA_ACP_SERVER) — e.g. https://your-paca-instance.example.com")
		return 2
	}

	// signal.NotifyContext, not the default SIGTERM-kills-immediately
	// behavior: a process manager stopping a backgrounded daemon sends
	// SIGTERM, and without this the connection (and whatever turn_status
	// was mid-flight) would be torn down with no chance to close cleanly
	// at all — this at least gives RunForever's own cleanup (closing the
	// WebSocket) a chance to run before the process exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("paca-acp-bridge starting", "version", version, "workspace", *workspace)

	client := bridge.New(*server, *agentID, *token, *workspace, log, runner.New(*workspace, log))
	if err := client.RunForever(ctx); err != nil && ctx.Err() == nil {
		log.Error("bridge exited with error", "error", err)
		return 1
	}
	return 0
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToUpper(level) {
	case "DEBUG":
		lvl = slog.LevelDebug
	case "WARN", "WARNING":
		lvl = slog.LevelWarn
	case "ERROR":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
