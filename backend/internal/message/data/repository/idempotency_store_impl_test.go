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

func TestIdempotencyStoreReserve(t *testing.T) {
	fp := entity.FingerprintOf("a", []byte(`{}`))
	record := entity.IdempotencyRecord{
		ProjectID: "p", Key: "k", Fingerprint: fp, MessageID: "msg_1", CreatedAt: storedAt.UTC(), ExpiresAt: storedAt.UTC().Add(time.Hour),
	}
	tests := []struct {
		name    string
		src     *stubKeys
		want    bool
		wantErr error
	}{
		{name: "owner", src: &stubKeys{won: true}, want: true},
		{name: "lost", src: &stubKeys{won: false}, want: false},
		{name: "storage error is wrapped", src: &stubKeys{err: errBoom}, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repositoryimpl.NewIdempotencyStore(tt.src).Reserve(context.Background(), record)

			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("got %v %v", got, err)
			}
			row := tt.src.reserved[0]
			if row.ProjectID != "p" || row.Key != "k" || row.MessageID != "msg_1" || string(row.Fingerprint) != string(fp[:]) || !row.ExpiresAt.Equal(record.ExpiresAt) {
				t.Fatalf("row not mapped: %+v", row)
			}
		})
	}
}

func TestIdempotencyStoreFind(t *testing.T) {
	fp := entity.FingerprintOf("a", []byte(`{}`))
	valid := model.IdempotencyKey{ProjectID: "p", Key: "k", Fingerprint: fp[:], MessageID: "msg_1", CreatedAt: storedAt, ExpiresAt: storedAt.Add(time.Hour)}
	tests := []struct {
		name    string
		src     *stubKeys
		wantErr error
	}{
		{name: "found", src: &stubKeys{row: valid}},
		{name: "not found becomes the domain error", src: &stubKeys{err: source.ErrNotFound}, wantErr: domainerrors.ErrNotFound},
		{name: "storage error is wrapped", src: &stubKeys{err: errBoom}, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repositoryimpl.NewIdempotencyStore(tt.src).Find(context.Background(), "p", "k")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.Fingerprint != fp || got.MessageID != "msg_1" || got.CreatedAt.Location() != time.UTC || !got.ExpiresAt.Equal(valid.ExpiresAt) {
				t.Fatalf("mapping wrong: %+v", got)
			}
		})
	}
}

func TestIdempotencyStoreFindRejectsACorruptFingerprint(t *testing.T) {
	src := &stubKeys{row: model.IdempotencyKey{Fingerprint: []byte{1, 2, 3}}}

	_, err := repositoryimpl.NewIdempotencyStore(src).Find(context.Background(), "p", "k")

	if err == nil {
		t.Fatal("a stored fingerprint of the wrong length must be an error, not a panic or a silent mismatch")
	}
}
