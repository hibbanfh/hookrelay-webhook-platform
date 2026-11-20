package repository

import (
	"context"

	"hookrelay/internal/endpoint/domain/entity"
)

// DemoSecretQuery selects the demo endpoints of one project.
type DemoSecretQuery struct {
	ProjectID   string
	ReceiverURL string
	Limit       int
}

// DemoSecretBatch is the answer of ListDemoSecrets. A secret that fails to open is reported in
// Unreadable and does not fail the batch, like in TargetBatch; the caller decides how to tell.
type DemoSecretBatch struct {
	Secrets    []entity.EndpointSecret
	Unreadable []UnreadableTarget
}

// SecretReader loads and decrypts endpoint signing secrets.
type SecretReader interface {
	// GetSecret returns domainerrors.ErrNotFound when the project has no such endpoint.
	GetSecret(ctx context.Context, projectID, id string) (entity.SigningSecret, error)
	// ListDemoSecrets returns the endpoints of the project whose url equals q.ReceiverURL or starts
	// with q.ReceiverURL + "?", oldest first, at most q.Limit. Secrets is never nil without an error.
	// A secret that fails to open goes to Unreadable: the worker cannot sign with it either, so no
	// request could ever verify against it.
	ListDemoSecrets(ctx context.Context, q DemoSecretQuery) (DemoSecretBatch, error)
}
