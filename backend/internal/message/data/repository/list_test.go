package repositoryimpl_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/message/data/model"
	repositoryimpl "hookrelay/internal/message/data/repository"
	"hookrelay/internal/message/data/source"
	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/repository"
)

func TestListMapsHeadersToUTCAndTranslatesTheQuery(t *testing.T) {
	anchor := entity.PageAnchor{CreatedAt: storedAt, ID: "msg_9"}
	tests := []struct {
		name string
		in   repository.ListQuery
		want source.ListParams
	}{
		{"first page", repository.ListQuery{ProjectID: "p", Limit: 3}, source.ListParams{ProjectID: "p", Limit: 3}},
		{"next page", repository.ListQuery{ProjectID: "p", Limit: 3, After: &anchor},
			source.ListParams{ProjectID: "p", Limit: 3, HasAfter: true, AfterCreatedAt: storedAt, AfterID: "msg_9"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &stubMessages{headers: []model.MessageHeader{{ID: "msg_1", ProjectID: "p", EventType: "a.b", CreatedAt: storedAt}}}

			got, err := repositoryimpl.NewMessageRepository(src).List(context.Background(), tt.in)

			if err != nil || len(got) != 1 || src.listIn != tt.want {
				t.Fatalf("params %+v, want %+v, rows %d, err %v", src.listIn, tt.want, len(got), err)
			}
			if got[0].ID != "msg_1" || got[0].EventType != "a.b" || got[0].CreatedAt.Location() != time.UTC || !got[0].CreatedAt.Equal(storedAt) {
				t.Fatalf("header = %+v", got[0])
			}
		})
	}
}

func TestListReturnsSourceErrors(t *testing.T) {
	_, err := repositoryimpl.NewMessageRepository(&stubMessages{err: errBoom}).List(context.Background(), repository.ListQuery{})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}
