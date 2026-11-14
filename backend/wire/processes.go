package wire

import (
	"log/slog"
	"net/http"

	"hookrelay/internal/dispatch/delivery/worker"
	projectuc "hookrelay/internal/project/domain/usecase"
)

// API is the object graph of the api subcommand.
type API struct {
	Handler http.Handler
	Log     *slog.Logger
}

// Worker is the object graph of the worker subcommand.
type Worker struct {
	Handler http.Handler
	Log     *slog.Logger
	Runner  *worker.Runner
}

// Seed is the object graph of the seed subcommand.
type Seed struct {
	DemoProject *projectuc.SeedDemoProject
	Log         *slog.Logger
}
