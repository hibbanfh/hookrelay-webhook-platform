package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/endpoint/domain/repository"
	"hookrelay/internal/endpoint/domain/usecase"
)

type fakeTargetReader struct {
	batch repository.TargetBatch
	err   error
	calls int
}

func (f *fakeTargetReader) GetTargets(context.Context, []string) (repository.TargetBatch, error) {
	f.calls++
	return f.batch, f.err
}

func TestGetDeliveryTargets(t *testing.T) {
	batch := repository.TargetBatch{Targets: []entity.DeliveryTarget{{EndpointID: "ep_1"}}, Unreadable: []repository.UnreadableTarget{{ID: "ep_2"}}}
	tests := []struct {
		name      string
		ids       []string
		reader    *fakeTargetReader
		wantCalls int
		wantErr   error
		wantLen   int
	}{
		{name: "returns the batch", ids: []string{"ep_1", "ep_2"}, reader: &fakeTargetReader{batch: batch}, wantCalls: 1, wantLen: 1},
		{name: "empty input makes no query", ids: nil, reader: &fakeTargetReader{batch: batch}, wantCalls: 0},
		{name: "wraps the reader error", ids: []string{"ep_1"}, reader: &fakeTargetReader{err: errBoom}, wantCalls: 1, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := usecase.NewGetDeliveryTargets(tt.reader)

			got, err := uc.Execute(context.Background(), usecase.GetDeliveryTargetsInput{EndpointIDs: tt.ids})

			if !errors.Is(err, tt.wantErr) || tt.reader.calls != tt.wantCalls || len(got.Targets) != tt.wantLen {
				t.Fatalf("err %v, calls %d, targets %d", err, tt.reader.calls, len(got.Targets))
			}
		})
	}
}
