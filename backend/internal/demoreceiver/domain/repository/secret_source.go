// Package repository holds the ports of the demoreceiver module. Two of them cross module
// boundaries and are implemented by adapters in backend/wire.
package repository

import (
	"context"

	"hookrelay/internal/demoreceiver/domain/entity"
)

// SecretSource lists the demo endpoints of a project with their signing keys. It is implemented
// in backend/wire over the endpoint module.
type SecretSource interface {
	// DemoSecrets returns the project's demo endpoints, oldest first. An unknown project, or one
	// without demo endpoints, returns an empty slice and no error.
	DemoSecrets(ctx context.Context, projectID string) ([]entity.Candidate, error)
}
