package usecase

import (
	"context"
	"fmt"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
)

// ClaimDueDeliveriesDeps are the collaborators of ClaimDueDeliveries.
type ClaimDueDeliveriesDeps struct {
	Queue     repository.DeliveryQueue
	Endpoints repository.EndpointCatalog
	Messages  repository.MessageCatalog
	IDs       IDGenerator
	Clock     Clock
}

// ClaimDueDeliveries leases the deliveries that are due and gathers what an attempt needs
// (internal, called by the worker). A batch costs three queries whatever its size.
type ClaimDueDeliveries struct {
	deps  ClaimDueDeliveriesDeps
	lease time.Duration
}

// NewClaimDueDeliveries builds the use case. lease is how long a claim holds a row.
func NewClaimDueDeliveries(deps ClaimDueDeliveriesDeps, lease time.Duration) *ClaimDueDeliveries {
	return &ClaimDueDeliveries{deps: deps, lease: lease}
}

// ClaimDueDeliveriesInput limits the claim to the worker's free slots.
type ClaimDueDeliveriesInput struct{ Limit int }

// ClaimDueDeliveriesOutput splits the claimed rows into ready jobs and skipped ones.
type ClaimDueDeliveriesOutput struct {
	Jobs    []entity.Job
	Skipped []entity.Skipped
}

// Execute claims up to in.Limit deliveries. Skipped ones stay leased and come back after the lease.
func (u *ClaimDueDeliveries) Execute(ctx context.Context, in ClaimDueDeliveriesInput) (ClaimDueDeliveriesOutput, error) {
	if in.Limit <= 0 {
		return ClaimDueDeliveriesOutput{}, nil
	}
	now := u.deps.Clock.Now()
	lease := entity.NewLease(u.deps.IDs.New(entity.LeaseTokenPrefix), now, u.lease)
	claimed, err := u.deps.Queue.ClaimDue(ctx, now, in.Limit, lease)
	if err != nil {
		return ClaimDueDeliveriesOutput{}, fmt.Errorf("claim due deliveries: %w", err)
	}
	if len(claimed) == 0 {
		return ClaimDueDeliveriesOutput{}, nil
	}
	return u.assemble(ctx, claimed)
}

func (u *ClaimDueDeliveries) assemble(ctx context.Context, claimed []entity.Delivery) (ClaimDueDeliveriesOutput, error) {
	targets, err := u.deps.Endpoints.GetTargets(ctx, distinctEndpointIDs(claimed))
	if err != nil {
		return ClaimDueDeliveriesOutput{}, fmt.Errorf("get delivery targets: %w", err)
	}
	payloads, err := u.deps.Messages.GetPayloads(ctx, distinctMessageIDs(claimed))
	if err != nil {
		return ClaimDueDeliveriesOutput{}, fmt.Errorf("get message payloads: %w", err)
	}
	return buildOutput(claimed, targets, payloads), nil
}

func buildOutput(claimed []entity.Delivery, targets repository.TargetLookup, payloads map[string][]byte) ClaimDueDeliveriesOutput {
	var out ClaimDueDeliveriesOutput
	for i := range claimed {
		job, reason, detail := buildJob(claimed[i], targets, payloads)
		if reason != "" {
			out.Skipped = append(out.Skipped, entity.Skipped{DeliveryID: claimed[i].ID, Reason: reason, Detail: detail})
			continue
		}
		out.Jobs = append(out.Jobs, job)
	}
	return out
}

// buildJob returns a non-empty reason when the delivery cannot be attempted, and a non-secret
// detail for the log when there is one.
func buildJob(d entity.Delivery, targets repository.TargetLookup, payloads map[string][]byte) (entity.Job, entity.SkipReason, string) {
	if cause, unreadable := targets.Unreadable[d.EndpointID]; unreadable {
		return entity.Job{}, entity.SkipSecretUnreadable, cause
	}
	target, ok := targets.Targets[d.EndpointID]
	if !ok {
		return entity.Job{}, entity.SkipTargetMissing, ""
	}
	payload, ok := payloads[d.MessageID]
	if !ok {
		return entity.Job{}, entity.SkipPayloadMissing, ""
	}
	return entity.Job{Delivery: d, Target: target, Payload: payload}, "", ""
}

func distinctEndpointIDs(ds []entity.Delivery) []string {
	return distinct(ds, func(d entity.Delivery) string { return d.EndpointID })
}

func distinctMessageIDs(ds []entity.Delivery) []string {
	return distinct(ds, func(d entity.Delivery) string { return d.MessageID })
}

func distinct(ds []entity.Delivery, key func(entity.Delivery) string) []string {
	seen := make(map[string]struct{}, len(ds))
	ids := make([]string, 0, len(ds))
	for i := range ds {
		id := key(ds[i])
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
