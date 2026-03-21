// Package logger provides a structured logger backed by log/slog.
package logger

import (
	"log/slog"
	"os"
)

// New returns a *slog.Logger configured for the given environment.
// In production, JSON output is used; otherwise, text output is used.
func New(env string) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
