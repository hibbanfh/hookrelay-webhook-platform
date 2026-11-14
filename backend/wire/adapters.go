package wire

import (
	"context"
	"fmt"

	dispatchentity "hookrelay/internal/dispatch/domain/entity"
	dispatchrepo "hookrelay/internal/dispatch/domain/repository"
	endpointrepository "hookrelay/internal/endpoint/domain/repository"
	endpointuc "hookrelay/internal/endpoint/domain/usecase"
	messageuc "hookrelay/internal/message/domain/usecase"
)

// This file is the only place where three modules meet (ADR 0014 section 4). dispatch declares
// the ports it needs; the adapters below implement them by calling internal use cases of the
// endpoint and message modules. No module imports another.

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
