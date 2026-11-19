package usecase_test

import (
	"context"
	"testing"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

func TestListDeliveryAttempts(t *testing.T) {
	tests := []struct {
		name        string
		in          usecase.ListDeliveryAttemptsInput
		rows        []entity.Attempt
		getErr      error
		listErr     error
		wantErr     error
		wantInvalid bool
		wantItems   int
		wantNext    int
		wantQueries int
	}{
		{name: "limit below one", in: usecase.ListDeliveryAttemptsInput{Limit: 0}, wantInvalid: true},
		{name: "negative cursor", in: usecase.ListDeliveryAttemptsInput{Limit: 1, AfterNumber: -1}, wantInvalid: true},
		{name: "unknown delivery", in: usecase.ListDeliveryAttemptsInput{Limit: 2}, getErr: domainerrors.ErrDeliveryNotFound, wantErr: domainerrors.ErrNotFound},
		{name: "get error is wrapped", in: usecase.ListDeliveryAttemptsInput{Limit: 2}, getErr: errStorage, wantErr: errStorage},
		{name: "list error is wrapped", in: usecase.ListDeliveryAttemptsInput{Limit: 2}, listErr: errStorage, wantErr: errStorage, wantQueries: 1},
		{name: "empty page", in: usecase.ListDeliveryAttemptsInput{Limit: 2}, wantQueries: 1},
		{name: "exactly limit rows has no next", in: usecase.ListDeliveryAttemptsInput{Limit: 2}, rows: attemptsNumbered(2), wantItems: 2, wantQueries: 1},
		{name: "limit plus one rows has next", in: usecase.ListDeliveryAttemptsInput{Limit: 2}, rows: attemptsNumbered(3), wantItems: 2, wantNext: 2, wantQueries: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deliveries := &fakeDeliveryReader{getErr: tt.getErr}
			attempts := &fakeAttemptReader{rows: tt.rows, err: tt.listErr}
			uc := usecase.NewListDeliveryAttempts(deliveries, attempts)

			out, err := uc.Execute(context.Background(), tt.in)

			assertListOutcome(t, err, tt.wantErr, tt.wantInvalid)
			if len(out.Attempts) != tt.wantItems || out.NextAfter != tt.wantNext {
				t.Fatalf("items %d next %d, want %d / %d", len(out.Attempts), out.NextAfter, tt.wantItems, tt.wantNext)
			}
			if len(attempts.calls) != tt.wantQueries {
				t.Fatalf("attempt queries = %d, want %d", len(attempts.calls), tt.wantQueries)
			}
		})
	}
}

func TestListDeliveryAttemptsQuery(t *testing.T) {
	attempts := &fakeAttemptReader{}
	uc := usecase.NewListDeliveryAttempts(&fakeDeliveryReader{}, attempts)

	_, err := uc.Execute(context.Background(), usecase.ListDeliveryAttemptsInput{ProjectID: "p", DeliveryID: "d", Limit: 5, AfterNumber: 3})

	got := attempts.calls[0]
	if err != nil || got.ProjectID != "p" || got.DeliveryID != "d" || got.AfterNumber != 3 || got.Limit != 6 {
		t.Fatalf("err %v, query %+v", err, got)
	}
}
