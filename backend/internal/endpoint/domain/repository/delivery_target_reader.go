package repository

import (
	"context"

	"hookrelay/internal/endpoint/domain/entity"
)

// UnreadableTarget is an endpoint whose secret could not be opened. Cause is safe to log: it
// names the key version and the class of failure, never ciphertext or key material.
type UnreadableTarget struct {
	ID    string
	Cause string
}

// TargetBatch is the answer of a batched read. An ID in neither list does not exist.
type TargetBatch struct {
	Targets    []entity.DeliveryTarget
	Unreadable []UnreadableTarget
}

// DeliveryTargetReader loads endpoints for the delivery worker. It is a system-level read with
// no project scope: the worker is not a tenant, and the composite foreign keys of deliveries
// guarantee that a delivery only points at an endpoint of its own project.
type DeliveryTargetReader interface {
	// GetTargets returns the endpoints that exist. A secret that fails to open puts the ID in
	// Unreadable and does not fail the batch.
	GetTargets(ctx context.Context, ids []string) (TargetBatch, error)
}
