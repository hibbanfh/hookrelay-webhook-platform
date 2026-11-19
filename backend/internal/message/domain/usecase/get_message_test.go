package usecase_test

import (
	"context"
	"testing"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/usecase"
)

func TestGetMessage(t *testing.T) {
	msg := entity.Message{ID: "msg_1", ProjectID: "p", EventType: "a", Payload: []byte(`{"b":1,  "a":2}`)}
	tests := []struct {
		name          string
		getErr        error
		deliveriesErr error
		wantErr       error
	}{
		{name: "found"},
		{name: "unknown message is not found", getErr: domainerrors.ErrNotFound, wantErr: domainerrors.ErrNotFound},
		{name: "get error is wrapped", getErr: errBoom, wantErr: errBoom},
		{name: "deliveries error is wrapped", deliveriesErr: errBoom, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &readMessages{fakeMessages: &fakeMessages{}, msg: msg, getErr: tt.getErr}
			deliveries := &fakeDeliveries{deliveries: []entity.MessageDelivery{{ID: "dlv_1"}}, deliveriesErr: tt.deliveriesErr}
			uc := usecase.NewGetMessage(repo, deliveries)

			out, err := uc.Execute(context.Background(), usecase.GetMessageInput{ProjectID: "p", ID: "msg_1"})

			assertReadErr(t, err, tt.wantErr, false)
			if tt.wantErr != nil {
				return
			}
			if string(out.Message.Payload) != string(msg.Payload) || len(out.Deliveries) != 1 {
				t.Fatalf("got %+v", out)
			}
		})
	}
}

func TestGetMessageDoesNotReadDeliveriesOfAMissingMessage(t *testing.T) {
	repo := &readMessages{fakeMessages: &fakeMessages{}, getErr: domainerrors.ErrNotFound}
	deliveries := &fakeDeliveries{}
	uc := usecase.NewGetMessage(repo, deliveries)

	_, _ = uc.Execute(context.Background(), usecase.GetMessageInput{ProjectID: "p", ID: "msg_x"})

	if deliveries.listCalls != 0 {
		t.Fatalf("deliveries were read %d times for a missing message", deliveries.listCalls)
	}
}
