// Package logger builds the single JSON slog logger used by every process.
package logger

import (
	"io"
	"log/slog"
)

// New returns a JSON logger tagged with the process name.
func New(w io.Writer, level slog.Level, process string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(h).With(slog.String("process", process))
}
