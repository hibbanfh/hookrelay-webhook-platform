package repositoryimpl_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/dispatch/data/model"
	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
)

type fakeReplaySource struct {
	row   model.DeliveryRow
	err   error
	got   model.ReplayRequest
	calls int
}

func (f *fakeReplaySource) Requeue(_ context.Context, req model.ReplayRequest) (model.DeliveryRow, error) {
	f.calls++
	f.got = req
	return f.row, f.err
}

func replayed(next *time.Time) entity.Delivery {
	return entity.Delivery{
		ID: "dlv_1", ProjectID: "proj_1", Status: entity.StatusPending, Trigger: entity.TriggerManual, NextAttemptAt: next,
	}
}

func TestDeliveryReplayerRequeue(t *testing.T) {
	local := time.FixedZone("UTC+7", 7*3600)
	due := time.Date(2026, 11, 19, 17, 0, 0, 0, local) // 10:00 UTC
	tests := []struct {
		name      string
		d         entity.Delivery
		srcErr    error
		wantErr   error
		wantCalls int
	}{
		{name: "stored", d: replayed(&due), wantCalls: 1},
		{name: "no finished row matched", d: replayed(&due), srcErr: source.ErrNoRows, wantErr: domainerrors.ErrDeliveryPending, wantCalls: 1},
		{name: "storage error is wrapped", d: replayed(&due), srcErr: errBoom, wantErr: errBoom, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeReplaySource{row: model.DeliveryRow{ID: "dlv_1", Status: "pending", Trigger: "manual"}, err: tt.srcErr}

			got, err := repositoryimpl.NewDeliveryReplayer(src).Requeue(context.Background(), tt.d)

			assertRequeue(t, src, got, err, tt.wantErr, tt.wantCalls)
		})
	}
}

func assertRequeue(t *testing.T, src *fakeReplaySource, got entity.Delivery, err, wantErr error, wantCalls int) {
	t.Helper()
	if src.calls != wantCalls {
		t.Fatalf("source calls = %d, want %d", src.calls, wantCalls)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if wantErr != nil {
		return
	}
	req := src.got
	if err != nil || got.ID != "dlv_1" || req.Status != "pending" || req.Trigger != "manual" || req.NextAttemptAt.Location() != time.UTC || req.NextAttemptAt.Hour() != 10 {
		t.Fatalf("err %v, delivery %+v, request %+v", err, got, req)
	}
}

func TestDeliveryReplayerRejectsADeliveryWithoutADueTime(t *testing.T) {
	src := &fakeReplaySource{}

	_, err := repositoryimpl.NewDeliveryReplayer(src).Requeue(context.Background(), replayed(nil))

	if err == nil || src.calls != 0 {
		t.Fatalf("err %v, source calls %d, want an error and no call", err, src.calls)
	}
}
