package usecase

import (
	"context"

	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
)

// checkURL runs the destination policy and turns a rejection into a validation error on the url
// field. The policy text is safe to show (see repository.URLPolicy).
func checkURL(ctx context.Context, policy repository.URLPolicy, rawURL string) error {
	if err := policy.Check(ctx, rawURL); err != nil {
		return &domainerrors.ValidationError{Field: "url", Reason: err.Error()}
	}
	return nil
}
