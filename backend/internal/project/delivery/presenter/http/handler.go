// Package httppresenter is the HTTP adapter of the project module: handlers, routes,
// middleware and the error table. The directory is named presenter/http; the package name
// avoids shadowing net/http.
package httppresenter

import (
	"context"

	"hookrelay/internal/platform/httpx"
	"hookrelay/internal/project/domain/entity"
	"hookrelay/internal/project/domain/usecase"
)

// Use cases as the handler sees them (small interfaces make handler tests trivial).
type (
	projectCreator interface {
		CheckEnabled() error
		Execute(ctx context.Context, in usecase.CreateProjectInput) (usecase.CreateProjectOutput, error)
	}
	currentProjectGetter interface {
		Execute(ctx context.Context, in usecase.GetCurrentProjectInput) (entity.Project, error)
	}
)

// Handler serves the project endpoints.
type Handler struct {
	create      projectCreator
	current     currentProjectGetter
	resp        httpx.Responder
	demoBaseURL string
}

// NewHandler builds the handler. demoBaseURL is "" when the demo receiver is disabled.
func NewHandler(create projectCreator, current currentProjectGetter, resp httpx.Responder, demoBaseURL string) *Handler {
	return &Handler{create: create, current: current, resp: resp, demoBaseURL: demoBaseURL}
}
