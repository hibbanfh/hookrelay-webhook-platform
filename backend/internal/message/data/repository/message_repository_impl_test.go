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
	domainerrors "hookrelay/internal/message/domain/errors"
)

func TestMessageRepositoryCreateMapsTheEntityToARow(t *testing.T) {
	src := &stubMessages{}
	repo := repositoryimpl.NewMessageRepository(src)
	payload := []byte(`{"b":1,  "a":2}`)
	m := entity.Message{ID: "msg_1", ProjectID: "p", EventType: "a", Payload: payload, CreatedAt: storedAt.UTC()}

	err := repo.Create(context.Background(), m)

	if err != nil || len(src.inserted) != 1 {
		t.Fatalf("err %v, inserted %+v", err, src.inserted)
	}
	got := src.inserted[0]
	want := model.Message{ID: "msg_1", ProjectID: "p", EventType: "a", Payload: payload, CreatedAt: storedAt.UTC()}
	if got.ID != want.ID || got.ProjectID != want.ProjectID || got.EventType != want.EventType ||
		string(got.Payload) != string(want.Payload) || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMessageRepositoryCreateWrapsStorageErrors(t *testing.T) {
	repo := repositoryimpl.NewMessageRepository(&stubMessages{err: errBoom})

	err := repo.Create(context.Background(), entity.Message{ID: "msg_1"})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestMessageRepositoryGet(t *testing.T) {
	found := model.Message{ID: "msg_1", ProjectID: "p", EventType: "a", Payload: []byte(`{}`), CreatedAt: storedAt}
	tests := []struct {
		name    string
		src     *stubMessages
		wantErr error
	}{
		{name: "found", src: &stubMessages{row: found}},
		{name: "not found becomes the domain error", src: &stubMessages{err: source.ErrNotFound}, wantErr: domainerrors.ErrNotFound},
		{name: "other errors are wrapped", src: &stubMessages{err: errBoom}, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repositoryimpl.NewMessageRepository(tt.src).Get(context.Background(), "p", "msg_1")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.ID != "msg_1" || string(got.Payload) != `{}` || got.CreatedAt.Location() != time.UTC || !got.CreatedAt.Equal(storedAt) {
				t.Fatalf("mapping wrong: %+v", got)
			}
		})
	}
}
