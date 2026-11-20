package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/demoreceiver/domain/entity"
	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
	"hookrelay/internal/demoreceiver/domain/repository"
)

// ReceiveWebhook is the demo receiver (POST /demo/receiver/{project_id}): it verifies a Standard
// Webhooks request against the signing secrets of the project's demo endpoints, counts it per
// (endpoint, webhook-id) and fails on purpose while the count is within the failure budget.
type ReceiveWebhook struct {
	secrets repository.SecretSource
	signer  repository.Signer
	hits    repository.HitCounter
	clock   Clock
}

// NewReceiveWebhook builds the use case.
func NewReceiveWebhook(s repository.SecretSource, sg repository.Signer, h repository.HitCounter, c Clock) *ReceiveWebhook {
	return &ReceiveWebhook{secrets: s, signer: sg, hits: h, clock: c}
}

// ReceiveWebhookInput is one request as the adapter read it: the raw body and the three headers,
// each empty when it was missing or repeated.
type ReceiveWebhookInput struct {
	ProjectID string
	// Plan is the validated failure budget: the adapter builds it with entity.NewFailurePlan before
	// it reads the body, so a bad fail_first is refused first.
	Plan    entity.FailurePlan
	Headers entity.WebhookHeaders
	Body    []byte
}

// Execute runs the checks from the cheapest to the most expensive, so a request that is refused
// early costs no query. Only a request that passed the signature check is counted.
func (u *ReceiveWebhook) Execute(ctx context.Context, in ReceiveWebhookInput) (entity.Receipt, error) {
	wh, err := entity.ParseSignedWebhook(in.Headers, in.Body)
	if err != nil {
		return entity.Receipt{}, err
	}
	now := u.clock.Now()
	if !wh.IsFresh(now) {
		return entity.Receipt{}, domainerrors.ErrStaleTimestamp
	}
	endpointID, err := u.verify(ctx, in.ProjectID, wh)
	if err != nil {
		return entity.Receipt{}, err
	}
	key := entity.HitKey{EndpointID: endpointID, WebhookID: wh.ID}
	n, err := u.hits.Increment(ctx, repository.HitIncrement{Key: key, At: now, ExpiredBefore: now.Add(-entity.HitRetention)})
	if err != nil {
		return entity.Receipt{}, fmt.Errorf("count request of %s: %w", endpointID, err)
	}
	if in.Plan.ShouldFail(n) {
		return entity.Receipt{}, &domainerrors.SimulatedFailure{RequestNumber: n, FailFirst: in.Plan.FailFirst()}
	}
	return entity.Receipt{EndpointID: endpointID, WebhookID: wh.ID, RequestNumber: n, FailFirst: in.Plan.FailFirst()}, nil
}

// verify returns the endpoint whose secret signed the request. A project without candidates
// fails the same way as a wrong signature, so the answer never says whether the project exists.
func (u *ReceiveWebhook) verify(ctx context.Context, projectID string, wh entity.SignedWebhook) (string, error) {
	candidates, err := u.secrets.DemoSecrets(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("load demo secrets of %s: %w", projectID, err)
	}
	endpointID, ok := u.matchCandidate(candidates, wh)
	if !ok {
		return "", domainerrors.ErrInvalidSignature
	}
	return endpointID, nil
}

// matchCandidate returns the first candidate whose signature of the request is in the header.
func (u *ReceiveWebhook) matchCandidate(candidates []entity.Candidate, wh entity.SignedWebhook) (string, bool) {
	for _, c := range candidates {
		expected := u.signer.Sign(c.Key.Bytes(), wh.ID, wh.Timestamp, wh.Body)
		if wh.Signatures.Contains(expected) {
			return c.EndpointID, true
		}
	}
	return "", false
}
