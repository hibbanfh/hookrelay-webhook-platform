package repositoryimpl_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/message/data/model"
	repositoryimpl "hookrelay/internal/message/data/repository"
)

func TestGetManyMapsRowsToEntitiesInUTC(t *testing.T) {
	stub := &stubMessages{rows: []model.Message{{ID: "msg_1", ProjectID: "p", EventType: "a", Payload: []byte(`{}`), CreatedAt: storedAt}}}

	got, err := repositoryimpl.NewMessageRepository(stub).GetMany(context.Background(), []string{"msg_1"})

	if err != nil || len(got) != 1 || got[0].CreatedAt.Location().String() != "UTC" || !got[0].CreatedAt.Equal(storedAt) {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestGetManyReturnsSourceErrors(t *testing.T) {
	_, err := repositoryimpl.NewMessageRepository(&stubMessages{err: errBoom}).GetMany(context.Background(), []string{"msg_1"})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}
