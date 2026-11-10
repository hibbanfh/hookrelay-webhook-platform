package logger

import (
	"io"
	"log/slog"

	"github.com/google/wire"
)

// ProviderSet provides the process logger.
var ProviderSet = wire.NewSet(Provide)

// Options configure the process logger.
type Options struct {
	Writer  io.Writer
	Level   slog.Level
	Process string
}

// Provide builds the JSON logger described by o.
func Provide(o Options) *slog.Logger { return New(o.Writer, o.Level, o.Process) }
