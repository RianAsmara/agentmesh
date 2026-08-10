// Package observability provides shared structured-logging setup for every
// AgentMesh service.
package observability

import (
	"io"
	"log/slog"
)

// NewLogger returns a JSON structured logger that writes to w at the given
// level, tagged with the given service name on every entry. Valid levels are
// "debug", "info", "warn", "error"; any other value (including "") defaults
// to "info".
func NewLogger(w io.Writer, service, level string) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(handler).With("service", service)
}

func parseLevel(level string) slog.Level {
	switch level {
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
