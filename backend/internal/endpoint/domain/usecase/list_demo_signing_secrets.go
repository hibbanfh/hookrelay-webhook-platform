package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/domain/repository"
)

// MaxDemoCandidates is how many demo endpoints one project may have, which is the endpoint limit.
// It bounds the signatures the demo receiver computes for one request.
const MaxDemoCandidates = 20

// ListDemoSigningSecrets lists the signing secrets of a project's demo endpoints for the demo
// receiver. It is internal: no HTTP route exposes it (ADR 0017 section 2).
type ListDemoSigningSecrets struct{ secrets repository.SecretReader }

// NewListDemoSigningSecrets builds the use case.
func NewListDemoSigningSecrets(secrets repository.SecretReader) *ListDemoSigningSecrets {
	return &ListDemoSigningSecrets{secrets: secrets}
}

// ListDemoSigningSecretsInput names the project and the exact receiver URL of that project. An
// empty ReceiverURL means the demo receiver is switched off.
type ListDemoSigningSecretsInput struct{ ProjectID, ReceiverURL string }

// Execute returns the secrets, oldest endpoint first, and the endpoints whose secret did not open.
// A disabled receiver makes no query.
func (u *ListDemoSigningSecrets) Execute(ctx context.Context, in ListDemoSigningSecretsInput) (repository.DemoSecretBatch, error) {
	if in.ReceiverURL == "" {
		return repository.DemoSecretBatch{}, nil
	}
	rows, err := u.secrets.ListDemoSecrets(ctx, repository.DemoSecretQuery{
		ProjectID: in.ProjectID, ReceiverURL: in.ReceiverURL, Limit: MaxDemoCandidates,
	})
	if err != nil {
		return repository.DemoSecretBatch{}, fmt.Errorf("list demo secrets of project %s: %w", in.ProjectID, err)
	}
	return rows, nil
}
