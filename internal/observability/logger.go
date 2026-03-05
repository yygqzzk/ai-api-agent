package observability

import (
	"io"
	"log/slog"
	"os"
)

// NewLogger creates a slog.Logger with JSON output.
// If w is nil, defaults to os.Stdout.
func NewLogger(w io.Writer, debug bool) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}
