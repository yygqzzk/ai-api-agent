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
		// os.Stdout 是默认标准输出；当调用方没有指定 writer 时，
		// 日志就会直接打印到当前进程的终端或容器日志流里。
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
