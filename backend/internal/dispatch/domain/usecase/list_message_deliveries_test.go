package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/usecase"
)

func TestListMessageDeliveries(t *testing.T) {
	tests := []struct {
		name    string
		rows    []entity.Delivery
		err     error
		want    int
		wantErr error
	}{
		{name: "none gives an empty slice", rows: nil, want: 0},
		{name: "rows are returned", rows: deliveriesAt(2), want: 2},
		{name: "reader error is wrapped", err: errStorage, wantErr: errStorage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeDeliveryReader{byMessage: tt.rows, listErr: tt.err}
			uc := usecase.NewListMessageDeliveries(reader)

			got, err := uc.Execute(context.Background(), usecase.ListMessageDeliveriesInput{ProjectID: "p", MessageID: "m"})

			if !errors.Is(err, tt.wantErr) || reader.msgCalls != 1 {
				t.Fatalf("err %v, calls %d", err, reader.msgCalls)
			}
			if tt.wantErr == nil && (got == nil || len(got) != tt.want) {
				t.Fatalf("got %v (nil=%v), want %d rows and a non-nil slice", got, got == nil, tt.want)
			}
		})
	}
}
