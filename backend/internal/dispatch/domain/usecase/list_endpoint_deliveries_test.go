package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

var errStorage = errors.New("storage down")

func TestListEndpointDeliveries(t *testing.T) {
	failed := entity.StatusFailed
	tests := []struct {
		name          string
		in            usecase.ListEndpointDeliveriesInput
		rows          []entity.Delivery
		stateErr      error
		listErr       error
		wantErr       error
		wantInvalid   bool
		wantItems     int
		wantNext      string
		wantListCalls int
		wantFetch     int
	}{
		{name: "limit below one", in: usecase.ListEndpointDeliveriesInput{Limit: 0}, wantInvalid: true},
		{name: "unknown endpoint", in: usecase.ListEndpointDeliveriesInput{Limit: 2}, stateErr: domainerrors.ErrEndpointNotFound, wantErr: domainerrors.ErrNotFound},
		{name: "state error is wrapped", in: usecase.ListEndpointDeliveriesInput{Limit: 2}, stateErr: errStorage, wantErr: errStorage},
		{name: "list error is wrapped", in: usecase.ListEndpointDeliveriesInput{Limit: 2}, listErr: errStorage, wantErr: errStorage, wantListCalls: 1, wantFetch: 3},
		{name: "empty page", in: usecase.ListEndpointDeliveriesInput{Limit: 2}, wantListCalls: 1, wantFetch: 3},
		{name: "exactly limit rows has no next", in: usecase.ListEndpointDeliveriesInput{Limit: 2}, rows: deliveriesAt(2), wantItems: 2, wantListCalls: 1, wantFetch: 3},
		{name: "limit plus one rows has next", in: usecase.ListEndpointDeliveriesInput{Limit: 2}, rows: deliveriesAt(3), wantItems: 2, wantNext: "dlv_b", wantListCalls: 1, wantFetch: 3},
		{name: "status filter is passed through", in: usecase.ListEndpointDeliveriesInput{Limit: 1, Status: &failed}, rows: deliveriesAt(1), wantItems: 1, wantListCalls: 1, wantFetch: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deliveries := &fakeDeliveryReader{list: tt.rows, listErr: tt.listErr}
			endpoints := &fakeEndpointState{err: tt.stateErr}
			uc := usecase.NewListEndpointDeliveries(deliveries, endpoints)

			out, err := uc.Execute(context.Background(), tt.in)

			assertListOutcome(t, err, tt.wantErr, tt.wantInvalid)
			if len(out.Deliveries) != tt.wantItems || nextID(out.Next) != tt.wantNext {
				t.Fatalf("items %d next %q, want %d / %q", len(out.Deliveries), nextID(out.Next), tt.wantItems, tt.wantNext)
			}
			if len(deliveries.listCalls) != tt.wantListCalls {
				t.Fatalf("list calls = %d, want %d", len(deliveries.listCalls), tt.wantListCalls)
			}
			if tt.wantListCalls > 0 && deliveries.listCalls[0].Limit != tt.wantFetch {
				t.Fatalf("fetched %d rows, want limit+1 = %d", deliveries.listCalls[0].Limit, tt.wantFetch)
			}
			if tt.wantListCalls > 0 && deliveries.listCalls[0].Status != tt.in.Status {
				t.Fatal("the status filter must reach the reader untouched")
			}
		})
	}
}

func TestListEndpointDeliveriesChecksOwnershipBeforeListing(t *testing.T) {
	deliveries := &fakeDeliveryReader{list: deliveriesAt(1)}
	endpoints := &fakeEndpointState{err: domainerrors.ErrEndpointNotFound}
	uc := usecase.NewListEndpointDeliveries(deliveries, endpoints)

	_, err := uc.Execute(context.Background(), usecase.ListEndpointDeliveriesInput{Limit: 1})

	if !errors.Is(err, domainerrors.ErrEndpointNotFound) || len(deliveries.listCalls) != 0 {
		t.Fatalf("err %v, list calls %d", err, len(deliveries.listCalls))
	}
}

func assertListOutcome(t *testing.T, err, wantErr error, wantInvalid bool) {
	t.Helper()
	if wantInvalid {
		var invalid *domainerrors.ValidationError
		if !errors.As(err, &invalid) || invalid.Field != "limit" && invalid.Field != "cursor" {
			t.Fatalf("err = %v, want a validation error", err)
		}
		return
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func nextID(a *entity.PageAnchor) string {
	if a == nil {
		return ""
	}
	return a.ID
}
