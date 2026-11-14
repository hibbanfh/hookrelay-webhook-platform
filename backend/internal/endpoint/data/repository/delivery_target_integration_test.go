//go:build integration

package repositoryimpl_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	repositoryimpl "hookrelay/internal/endpoint/data/repository"
	"hookrelay/internal/endpoint/data/source"
	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/library/secretbox"
)

func TestDeliveryTargetsAcrossProjectsMatchTheStoredEndpoints(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	secretA := h.create(t, projectA, epID(1), time.Now())
	h.create(t, projectB, epID(2), time.Now())
	if _, err := h.pool.Exec(ctx, `UPDATE endpoints SET enabled = false WHERE id = $1`, epID(2)); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New(secretbox.Options{Key: bytes.Repeat([]byte{1}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	reader := repositoryimpl.NewDeliveryTargetReader(source.NewEndpointPersistent(h.pool), box)

	batch, err := reader.GetTargets(ctx, []string{epID(1), epID(2), epID(404)})

	if err != nil || len(batch.Targets) != 2 || len(batch.Unreadable) != 0 {
		t.Fatalf("targets %d, unreadable %v, err %v", len(batch.Targets), batch.Unreadable, err)
	}
	byID := map[string]entity.DeliveryTarget{}
	for _, tg := range batch.Targets {
		byID[tg.EndpointID] = tg
	}
	if _, found := byID[epID(404)]; found {
		t.Fatal("an unknown ID must be absent")
	}
	if got := byID[epID(1)]; !bytes.Equal(got.Secret.Bytes(), secretA.Bytes()) || !got.Enabled || got.ProjectID != projectA {
		t.Fatalf("endpoint 1 = %+v", got)
	}
	if got := byID[epID(2)]; got.Enabled || got.ProjectID != projectB {
		t.Fatalf("the disabled endpoint of project B must come back with enabled=false: %+v", got)
	}
}

func TestDeliveryTargetsReportASecretSealedWithAnotherKeyAsUnreadable(t *testing.T) {
	h := start(t)
	h.create(t, projectA, epID(1), time.Now())
	otherBox, err := secretbox.New(secretbox.Options{Key: bytes.Repeat([]byte{2}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	reader := repositoryimpl.NewDeliveryTargetReader(source.NewEndpointPersistent(h.pool), otherBox)

	batch, err := reader.GetTargets(context.Background(), []string{epID(1)})

	if err != nil || len(batch.Targets) != 0 || len(batch.Unreadable) != 1 {
		t.Fatalf("targets %d, unreadable %v, err %v", len(batch.Targets), batch.Unreadable, err)
	}
}
