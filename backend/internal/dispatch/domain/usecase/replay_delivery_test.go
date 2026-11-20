package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

func finished(status entity.Status) entity.Delivery {
	last := t0.Add(-time.Hour)
	return entity.Delivery{
		ID: "dlv_1", ProjectID: "proj_1", MessageID: "msg_1", EndpointID: "ep_1",
		Status: status, Trigger: entity.TriggerAutomatic, AttemptCount: 6, LastAttemptAt: &last, CreatedAt: t0.Add(-2 * time.Hour),
	}
}

func TestReplayDelivery(t *testing.T) {
	waiting := finished(entity.StatusPending)
	due := t0.Add(time.Minute)
	waiting.NextAttemptAt = &due
	tests := []struct {
		name         string
		delivery     entity.Delivery
		getErr       error
		disabled     bool
		stateErr     error
		requeueErr   error
		wantErr      error
		wantStates   int
		wantRequeues int
	}{
		{name: "failed is replayed", delivery: finished(entity.StatusFailed), wantStates: 1, wantRequeues: 1},
		{name: "succeeded is replayed", delivery: finished(entity.StatusSucceeded), wantStates: 1, wantRequeues: 1},
		{name: "unknown or foreign delivery", getErr: domainerrors.ErrDeliveryNotFound, wantErr: domainerrors.ErrDeliveryNotFound},
		{name: "delivery lookup fails", getErr: errStorage, wantErr: errStorage},
		{name: "endpoint disabled", delivery: finished(entity.StatusFailed), disabled: true, wantErr: domainerrors.ErrEndpointDisabled, wantStates: 1},
		{name: "endpoint disabled wins over pending", delivery: waiting, disabled: true, wantErr: domainerrors.ErrEndpointDisabled, wantStates: 1},
		{name: "pending delivery", delivery: waiting, wantErr: domainerrors.ErrDeliveryPending, wantStates: 1},
		{name: "endpoint lookup fails", delivery: finished(entity.StatusFailed), stateErr: errStorage, wantErr: errStorage, wantStates: 1},
		{name: "lost race", delivery: finished(entity.StatusFailed), requeueErr: domainerrors.ErrDeliveryPending, wantErr: domainerrors.ErrDeliveryPending, wantStates: 1, wantRequeues: 1},
		{name: "requeue fails", delivery: finished(entity.StatusFailed), requeueErr: errStorage, wantErr: errStorage, wantStates: 1, wantRequeues: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deliveries := &fakeDeliveryReader{get: tt.delivery, getErr: tt.getErr}
			endpoints := &fakeEndpointState{disabled: tt.disabled, err: tt.stateErr}
			replayer := &fakeReplayer{err: tt.requeueErr}
			uc := usecase.NewReplayDelivery(deliveries, endpoints, replayer, fixedClock(t0))

			_, err := uc.Execute(context.Background(), usecase.ReplayDeliveryInput{ProjectID: "proj_1", DeliveryID: "dlv_1"})

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if endpoints.calls != tt.wantStates || len(replayer.got) != tt.wantRequeues {
				t.Fatalf("endpoint lookups %d (want %d), requeues %d (want %d)", endpoints.calls, tt.wantStates, len(replayer.got), tt.wantRequeues)
			}
		})
	}
}

// The stored row differs from the requested one (another ID), so the output must be the stored one.
func TestReplayDeliveryRequeuesTheReplayedStateAndReturnsTheStoredRow(t *testing.T) {
	stored := finished(entity.StatusPending)
	stored.ID = "dlv_stored"
	replayer := &fakeReplayer{stored: stored}
	endpoints := &fakeEndpointState{}
	uc := usecase.NewReplayDelivery(&fakeDeliveryReader{get: finished(entity.StatusFailed)}, endpoints, replayer, fixedClock(t0))

	out, err := uc.Execute(context.Background(), usecase.ReplayDeliveryInput{ProjectID: "proj_1", DeliveryID: "dlv_1"})

	if err != nil || len(replayer.got) != 1 {
		t.Fatalf("err %v, requeues %d, want none and 1", err, len(replayer.got))
	}
	got := replayer.got[0]
	if out.Delivery.ID != "dlv_stored" || out.Delivery.Status != entity.StatusPending {
		t.Fatalf("output %+v, want the stored row", out.Delivery)
	}
	if got.ID != "dlv_1" || got.ProjectID != "proj_1" || got.Status != entity.StatusPending || got.Trigger != entity.TriggerManual ||
		got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(t0) || got.AttemptCount != 6 {
		t.Fatalf("requeued %+v", got)
	}
	if endpoints.gotProject != "proj_1" || endpoints.gotEndpoint != "ep_1" {
		t.Fatalf("endpoint state asked for %s/%s", endpoints.gotProject, endpoints.gotEndpoint)
	}
}
