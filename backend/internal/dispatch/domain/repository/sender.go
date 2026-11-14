package repository

import (
	"context"

	"hookrelay/internal/dispatch/domain/entity"
)

// Sender performs the outbound HTTP call.
type Sender interface {
	// Send delivers req. Every network failure is an Outcome. The only error is a wrapped
	// domainerrors.ErrSendAborted, returned when ctx ended before the attempt finished.
	Send(ctx context.Context, req entity.SignedRequest) (entity.Outcome, error)
}
