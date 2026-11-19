package wire

import (
	"context"
	"errors"
	"fmt"

	dispatchentity "hookrelay/internal/dispatch/domain/entity"
	dispatcherrors "hookrelay/internal/dispatch/domain/errors"
	dispatchrepo "hookrelay/internal/dispatch/domain/repository"
	dispatchuc "hookrelay/internal/dispatch/domain/usecase"
	endpointerrors "hookrelay/internal/endpoint/domain/errors"
	endpointrepository "hookrelay/internal/endpoint/domain/repository"
	endpointuc "hookrelay/internal/endpoint/domain/usecase"
	messageentity "hookrelay/internal/message/domain/entity"
	messagerepository "hookrelay/internal/message/domain/repository"
	messageuc "hookrelay/internal/message/domain/usecase"
)

// This file is the only place where three modules meet (ADR 0014 section 4). dispatch declares
// the ports it needs, and message declares the one it reads deliveries through (ADR 0015); the
// adapters below implement them by calling internal use cases of the other modules. No module
// imports another.

// endpointCatalog implements dispatch's EndpointCatalog over the endpoint module.
type endpointCatalog struct {
	targets *endpointuc.GetDeliveryTargets
}

// ProvideEndpointCatalog adapts the endpoint use case to the dispatch port.
func ProvideEndpointCatalog(uc *endpointuc.GetDeliveryTargets) dispatchrepo.EndpointCatalog {
	return endpointCatalog{targets: uc}
}

// GetTargets maps the endpoint module's batch to dispatch targets. The decrypted secret crosses
// the boundary as raw key bytes wrapped in a SigningKey, which redacts itself.
func (c endpointCatalog) GetTargets(ctx context.Context, endpointIDs []string) (dispatchrepo.TargetLookup, error) {
	batch, err := c.targets.Execute(ctx, endpointuc.GetDeliveryTargetsInput{EndpointIDs: endpointIDs})
	if err != nil {
		return dispatchrepo.TargetLookup{}, fmt.Errorf("read endpoints: %w", err)
	}
	return toTargetLookup(batch), nil
}

func toTargetLookup(batch endpointrepository.TargetBatch) dispatchrepo.TargetLookup {
	lookup := dispatchrepo.TargetLookup{
		Targets:    make(map[string]dispatchentity.Target, len(batch.Targets)),
		Unreadable: make(map[string]string, len(batch.Unreadable)),
	}
	for _, t := range batch.Targets {
		lookup.Targets[t.EndpointID] = dispatchentity.Target{
			EndpointID: t.EndpointID, ProjectID: t.ProjectID, URL: t.URL, Enabled: t.Enabled,
			Key: dispatchentity.NewSigningKey(t.Secret.Bytes()),
		}
	}
	for _, u := range batch.Unreadable {
		lookup.Unreadable[u.ID] = u.Cause
	}
	return lookup
}

// messageCatalog implements dispatch's MessageCatalog over the message module.
type messageCatalog struct{ payloads *messageuc.GetMessagePayloads }

// ProvideMessageCatalog adapts the message use case to the dispatch port.
func ProvideMessageCatalog(uc *messageuc.GetMessagePayloads) dispatchrepo.MessageCatalog {
	return messageCatalog{payloads: uc}
}

// GetPayloads returns the stored payload bytes keyed by message ID, untouched.
func (c messageCatalog) GetPayloads(ctx context.Context, messageIDs []string) (map[string][]byte, error) {
	messages, err := c.payloads.Execute(ctx, messageuc.GetMessagePayloadsInput{IDs: messageIDs})
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}
	out := make(map[string][]byte, len(messages))
	for _, m := range messages {
		out[m.ID] = m.Payload
	}
	return out, nil
}

// deliveryReader implements message's DeliveryReader over the dispatch module (ADR 0015): the
// message module shows deliveries but does not own them.
type deliveryReader struct {
	summarize *dispatchuc.SummarizeDeliveries
	list      *dispatchuc.ListMessageDeliveries
}

// ProvideDeliveryReader adapts the two internal dispatch use cases to the message port.
func ProvideDeliveryReader(s *dispatchuc.SummarizeDeliveries, l *dispatchuc.ListMessageDeliveries) messagerepository.DeliveryReader {
	return deliveryReader{summarize: s, list: l}
}

// Summaries maps dispatch status counts to message delivery summaries. Every requested ID is in the result.
func (r deliveryReader) Summaries(ctx context.Context, projectID string, messageIDs []string) (map[string]messageentity.DeliverySummary, error) {
	counts, err := r.summarize.Execute(ctx, dispatchuc.SummarizeDeliveriesInput{ProjectID: projectID, MessageIDs: messageIDs})
	if err != nil {
		return nil, fmt.Errorf("summarize deliveries: %w", err)
	}
	out := make(map[string]messageentity.DeliverySummary, len(counts))
	for id, c := range counts {
		out[id] = messageentity.DeliverySummary{Pending: c.Pending, Succeeded: c.Succeeded, Failed: c.Failed}
	}
	return out, nil
}

// ListForMessage maps dispatch deliveries to the message view of a delivery.
func (r deliveryReader) ListForMessage(ctx context.Context, projectID, messageID string) ([]messageentity.MessageDelivery, error) {
	deliveries, err := r.list.Execute(ctx, dispatchuc.ListMessageDeliveriesInput{ProjectID: projectID, MessageID: messageID})
	if err != nil {
		return nil, fmt.Errorf("list message deliveries: %w", err)
	}
	out := make([]messageentity.MessageDelivery, 0, len(deliveries))
	for i := range deliveries {
		out = append(out, toMessageDelivery(&deliveries[i]))
	}
	return out, nil
}

// endpointStateReader implements dispatch's EndpointStateReader over the endpoint module.
type endpointStateReader struct{ endpoints *endpointuc.GetEndpoint }

// ProvideEndpointStateReader adapts the existing GetEndpoint use case, which is project-scoped
// and never loads the secret, to the dispatch port.
func ProvideEndpointStateReader(uc *endpointuc.GetEndpoint) dispatchrepo.EndpointStateReader {
	return endpointStateReader{endpoints: uc}
}

// GetState reports whether the endpoint is enabled. This adapter is the one place where the
// endpoint module not-found error becomes the dispatch one, so each module error table knows only
// its own errors.
func (r endpointStateReader) GetState(ctx context.Context, projectID, endpointID string) (dispatchentity.EndpointState, error) {
	e, err := r.endpoints.Execute(ctx, endpointuc.GetEndpointInput{ProjectID: projectID, ID: endpointID})
	if errors.Is(err, endpointerrors.ErrNotFound) {
		return dispatchentity.EndpointState{}, dispatcherrors.ErrEndpointNotFound
	}
	if err != nil {
		return dispatchentity.EndpointState{}, fmt.Errorf("read endpoint: %w", err)
	}
	return dispatchentity.EndpointState{Enabled: e.Enabled}, nil
}

func toMessageDelivery(d *dispatchentity.Delivery) messageentity.MessageDelivery {
	return messageentity.MessageDelivery{
		ID: d.ID, MessageID: d.MessageID, EndpointID: d.EndpointID, Status: string(d.Status), AttemptCount: d.AttemptCount,
		NextAttemptAt: d.NextAttemptAt, LastAttemptAt: d.LastAttemptAt, CreatedAt: d.CreatedAt,
	}
}
