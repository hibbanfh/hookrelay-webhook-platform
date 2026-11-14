package entity_test

import (
	"testing"

	"hookrelay/internal/message/domain/entity"
)

func TestNewPendingDelivery(t *testing.T) {
	m, err := entity.NewMessage(spec("a", []byte(`{}`)), now)
	if err != nil {
		t.Fatal(err)
	}

	d := entity.NewPendingDelivery("dlv_1", m, "ep_1")

	want := entity.PendingDelivery{ID: "dlv_1", ProjectID: "proj_1", MessageID: "msg_1", EndpointID: "ep_1", CreatedAt: m.CreatedAt}
	if d != want {
		t.Fatalf("got %+v, want %+v", d, want)
	}
}
