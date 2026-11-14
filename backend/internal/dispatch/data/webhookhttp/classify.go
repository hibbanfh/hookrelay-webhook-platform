package webhookhttp

import (
	"context"
	"errors"
	"fmt"
	"net"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/library/netguard"
)

// classify turns a transport error into an outcome. The order matters: the caller's own
// cancellation comes first, because a shutdown must never be recorded as the receiver's timeout.
// Only errors.Is and errors.As are used, never the error text.
func classify(parent context.Context, err error) (entity.Outcome, error) {
	if parent.Err() != nil {
		return entity.Outcome{}, fmt.Errorf("send: %w", domainerrors.ErrSendAborted)
	}
	return entity.FailedOutcome(attemptErrorOf(err)), nil
}

func attemptErrorOf(err error) entity.AttemptError {
	var netErr net.Error
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, netguard.ErrDestinationBlocked):
		return entity.ErrorDestinationBlocked
	case errors.As(err, &dnsErr): // before the timeout case: a resolver that gives up is a DNS problem
		return entity.ErrorDNS
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return entity.ErrorTimeout
	}
	return entity.ErrorConnection
}
