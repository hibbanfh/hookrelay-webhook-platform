package repositoryimpl_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"hookrelay/internal/message/data/model"
	repositoryimpl "hookrelay/internal/message/data/repository"
	"hookrelay/internal/message/domain/entity"
)

func TestEndpointDirectoryReturnsTheSourceIDs(t *testing.T) {
	src := &stubFanout{ids: []string{"ep_1", "ep_2"}}

	got, err := repositoryimpl.NewEndpointDirectory(src).EnabledEndpointIDs(context.Background(), "p")

	if err != nil || !slices.Equal(got, []string{"ep_1", "ep_2"}) {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestEndpointDirectoryWrapsStorageErrors(t *testing.T) {
	_, err := repositoryimpl.NewEndpointDirectory(&stubFanout{err: errBoom}).EnabledEndpointIDs(context.Background(), "p")

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestDeliveryOutboxMapsEveryDeliveryToARow(t *testing.T) {
	src := &stubFanout{}
	at := storedAt.UTC()
	deliveries := []entity.PendingDelivery{
		{ID: "dlv_1", ProjectID: "p", MessageID: "msg_1", EndpointID: "ep_1", CreatedAt: at},
		{ID: "dlv_2", ProjectID: "p", MessageID: "msg_1", EndpointID: "ep_2", CreatedAt: at},
	}

	err := repositoryimpl.NewDeliveryOutbox(src).Enqueue(context.Background(), deliveries)

	want := []model.PendingDelivery{
		{ID: "dlv_1", ProjectID: "p", MessageID: "msg_1", EndpointID: "ep_1", CreatedAt: at},
		{ID: "dlv_2", ProjectID: "p", MessageID: "msg_1", EndpointID: "ep_2", CreatedAt: at},
	}
	if err != nil || !slices.EqualFunc(src.inserted, want, sameDeliveryRow) {
		t.Fatalf("err %v, inserted %+v", err, src.inserted)
	}
}

func sameDeliveryRow(a, b model.PendingDelivery) bool {
	return a.ID == b.ID && a.ProjectID == b.ProjectID && a.MessageID == b.MessageID &&
		a.EndpointID == b.EndpointID && a.CreatedAt.Equal(b.CreatedAt)
}

func TestDeliveryOutboxWrapsStorageErrors(t *testing.T) {
	err := repositoryimpl.NewDeliveryOutbox(&stubFanout{err: errBoom}).Enqueue(context.Background(), []entity.PendingDelivery{{ID: "dlv_1"}})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}
