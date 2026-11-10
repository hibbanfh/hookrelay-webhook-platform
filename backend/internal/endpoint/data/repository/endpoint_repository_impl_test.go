package repositoryimpl_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"hookrelay/internal/endpoint/data/model"
	repositoryimpl "hookrelay/internal/endpoint/data/repository"
	"hookrelay/internal/endpoint/data/source"
	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
)

var (
	errBoom = errors.New("boom")
	created = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
)

// fakeSource records the calls and returns canned results.
type fakeSource struct {
	calls     []string
	row       model.Endpoint
	sealed    model.EndpointSecret
	inserted  model.EndpointSecret
	gotList   source.ListParams
	gotUpdate source.UpdateParams
	rows      []model.Endpoint

	insertErr, getErr, secretErr, listErr, updateErr, lockErr, countErr error
	count                                                               int
}

func (f *fakeSource) Insert(_ context.Context, _ model.Endpoint, s model.EndpointSecret) error {
	f.inserted = s
	return f.insertErr
}

func (f *fakeSource) GetByID(context.Context, string, string) (model.Endpoint, error) {
	return f.row, f.getErr
}

func (f *fakeSource) GetSecretByID(context.Context, string, string) (model.EndpointSecret, error) {
	return f.sealed, f.secretErr
}

func (f *fakeSource) List(_ context.Context, p source.ListParams) ([]model.Endpoint, error) {
	f.gotList = p
	return f.rows, f.listErr
}

func (f *fakeSource) Update(_ context.Context, _, _ string, p source.UpdateParams) (model.Endpoint, error) {
	f.gotUpdate = p
	return f.row, f.updateErr
}

func (f *fakeSource) LockProject(context.Context, string) error {
	f.calls = append(f.calls, "lock")
	return f.lockErr
}

func (f *fakeSource) CountByProject(context.Context, string) (int, error) {
	f.calls = append(f.calls, "count")
	return f.count, f.countErr
}

// fakeCipher reverses the plaintext so that sealed bytes visibly differ, and records the AAD.
type fakeCipher struct {
	sealErr, openErr error
	gotAAD           []byte
	gotVersion       int
}

func (f *fakeCipher) Seal(plaintext, aad []byte) ([]byte, int, error) {
	f.gotAAD = aad
	return append([]byte("sealed:"), plaintext...), 7, f.sealErr
}

func (f *fakeCipher) Open(ciphertext, aad []byte, version int) ([]byte, error) {
	f.gotAAD, f.gotVersion = aad, version
	return bytes.TrimPrefix(ciphertext, []byte("sealed:")), f.openErr
}

func rowFixture() model.Endpoint {
	return model.Endpoint{ID: "ep_1", ProjectID: "proj_1", URL: "https://example.com/hook", Description: "d", Enabled: true,
		CreatedAt: created.In(time.FixedZone("x", 3600)), UpdatedAt: created}
}

func TestGetTranslatesErrorsAndMapsToUTC(t *testing.T) {
	tests := []struct {
		name     string
		getErr   error
		notFound bool
		wantErr  error
	}{
		{name: "found", getErr: nil},
		{name: "not found becomes the domain error", getErr: source.ErrNotFound, wantErr: domainerrors.ErrNotFound, notFound: true},
		{name: "other errors are wrapped", getErr: errBoom, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := repositoryimpl.NewEndpointRepository(&fakeSource{row: rowFixture(), getErr: tt.getErr}, &fakeCipher{})

			got, err := repo.Get(context.Background(), "proj_1", "ep_1")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (got.CreatedAt.Location() != time.UTC || !got.CreatedAt.Equal(created)) {
				t.Fatalf("created_at %v must be UTC", got.CreatedAt)
			}
			if errors.Is(err, domainerrors.ErrNotFound) != tt.notFound {
				t.Fatalf("not-found classification of %v must be %v", err, tt.notFound)
			}
		})
	}
}

func TestCreateSealsTheSecretWithTheEndpointIDAsAAD(t *testing.T) {
	src, cipher := &fakeSource{}, &fakeCipher{}
	secret, err := entity.NewSigningSecret(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	e := entity.Endpoint{ID: "ep_1", ProjectID: "proj_1", URL: "https://example.com", CreatedAt: created, UpdatedAt: created}

	err = repositoryimpl.NewEndpointRepository(src, cipher).Create(context.Background(), e, secret)

	plain := secret.Value()
	if err != nil || string(cipher.gotAAD) != "ep_1" || src.inserted.KeyVersion != 7 ||
		!bytes.HasPrefix(src.inserted.Ciphertext, []byte("sealed:")) || bytes.Contains(src.inserted.Ciphertext, []byte(plain)) {
		t.Fatalf("aad %q, stored %+v, err %v", cipher.gotAAD, src.inserted, err)
	}
}

func TestCreateFailures(t *testing.T) {
	tests := []struct {
		name   string
		src    *fakeSource
		cipher *fakeCipher
	}{
		{name: "sealing fails", src: &fakeSource{}, cipher: &fakeCipher{sealErr: errBoom}},
		{name: "insert fails", src: &fakeSource{insertErr: errBoom}, cipher: &fakeCipher{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, err := entity.NewSigningSecret(make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}

			err = repositoryimpl.NewEndpointRepository(tt.src, tt.cipher).Create(context.Background(), entity.Endpoint{ID: "ep_1"}, secret)

			if !errors.Is(err, errBoom) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestListPassesTheCursorAndMapsRows(t *testing.T) {
	anchor := &entity.PageAnchor{CreatedAt: created, ID: "ep_9"}
	tests := []struct {
		name      string
		after     *entity.PageAnchor
		wantAfter bool
	}{
		{name: "first page", after: nil, wantAfter: false},
		{name: "next page", after: anchor, wantAfter: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeSource{rows: []model.Endpoint{rowFixture()}}
			repo := repositoryimpl.NewEndpointRepository(src, &fakeCipher{})

			got, err := repo.List(context.Background(), repository.ListQuery{ProjectID: "proj_1", Limit: 6, After: tt.after})

			isAnchored := src.gotList.HasAfter && src.gotList.AfterID == "ep_9" && src.gotList.AfterCreatedAt.Equal(created)
			if err != nil || len(got) != 1 || src.gotList.Limit != 6 || src.gotList.ProjectID != "proj_1" || src.gotList.HasAfter != tt.wantAfter || (tt.wantAfter && !isAnchored) {
				t.Fatalf("got %v, params %+v, err %v", got, src.gotList, err)
			}
		})
	}
}

func TestListWrapsStorageErrors(t *testing.T) {
	repo := repositoryimpl.NewEndpointRepository(&fakeSource{listErr: errBoom}, &fakeCipher{})

	_, err := repo.List(context.Background(), repository.ListQuery{ProjectID: "proj_1", Limit: 1})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestUpdateFieldsPassesOnlyThePresentFields(t *testing.T) {
	src := &fakeSource{row: rowFixture()}
	off := false

	_, err := repositoryimpl.NewEndpointRepository(src, &fakeCipher{}).UpdateFields(
		context.Background(), "proj_1", "ep_1", repository.EndpointUpdate{Patch: entity.Patch{Enabled: &off}, At: created})

	p := src.gotUpdate
	if err != nil || p.URL != nil || p.Description != nil || p.Enabled == nil || *p.Enabled || !p.UpdatedAt.Equal(created) {
		t.Fatalf("params %+v, err %v", p, err)
	}
}

func TestUpdateFieldsTranslatesNotFound(t *testing.T) {
	repo := repositoryimpl.NewEndpointRepository(&fakeSource{updateErr: source.ErrNotFound}, &fakeCipher{})

	_, err := repo.UpdateFields(context.Background(), "proj_2", "ep_1", repository.EndpointUpdate{At: created})

	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestCountForUpdateLocksThenCountsAsSeparateStatements(t *testing.T) {
	tests := []struct {
		name      string
		src       *fakeSource
		wantCount int
		wantErr   error
		wantCalls []string
	}{
		{name: "locks then counts", src: &fakeSource{count: 4}, wantCount: 4, wantCalls: []string{"lock", "count"}},
		{name: "unknown project", src: &fakeSource{lockErr: source.ErrNotFound}, wantErr: domainerrors.ErrNotFound, wantCalls: []string{"lock"}},
		{name: "lock fails", src: &fakeSource{lockErr: errBoom}, wantErr: errBoom, wantCalls: []string{"lock"}},
		{name: "count fails", src: &fakeSource{countErr: errBoom}, wantErr: errBoom, wantCalls: []string{"lock", "count"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := repositoryimpl.NewEndpointRepository(tt.src, &fakeCipher{})

			got, err := repo.CountForUpdate(context.Background(), "proj_1")

			if got != tt.wantCount || !errors.Is(err, tt.wantErr) || !slices.Equal(tt.src.calls, tt.wantCalls) {
				t.Fatalf("count %d, err %v, calls %v", got, err, tt.src.calls)
			}
		})
	}
}

func TestGetSecretOpensWithTheEndpointIDAsAAD(t *testing.T) {
	tests := []struct {
		name      string
		src       *fakeSource
		cipher    *fakeCipher
		wantErr   error
		wantValue bool
	}{
		{name: "found", src: &fakeSource{sealed: model.EndpointSecret{Ciphertext: append([]byte("sealed:"), bytes.Repeat([]byte{3}, 32)...), KeyVersion: 7}}, cipher: &fakeCipher{}, wantValue: true},
		{name: "not found", src: &fakeSource{secretErr: source.ErrNotFound}, cipher: &fakeCipher{}, wantErr: domainerrors.ErrNotFound},
		{name: "storage failure", src: &fakeSource{secretErr: errBoom}, cipher: &fakeCipher{}, wantErr: errBoom},
		{name: "cannot decrypt", src: &fakeSource{sealed: model.EndpointSecret{Ciphertext: []byte("x"), KeyVersion: 7}}, cipher: &fakeCipher{openErr: errBoom}, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, err := repositoryimpl.NewSecretReader(tt.src, tt.cipher).GetSecret(context.Background(), "proj_1", "ep_1")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if tt.wantValue && (len(secret.Bytes()) != 32 || tt.cipher.gotVersion != 7 || string(tt.cipher.gotAAD) != "ep_1") {
				t.Fatalf("aad %q, version %d", tt.cipher.gotAAD, tt.cipher.gotVersion)
			}
		})
	}
}

func TestGetSecretRejectsADecryptedKeyThatIsTooShort(t *testing.T) {
	src := &fakeSource{sealed: model.EndpointSecret{Ciphertext: []byte("sealed:abc"), KeyVersion: 7}}

	_, err := repositoryimpl.NewSecretReader(src, &fakeCipher{}).GetSecret(context.Background(), "proj_1", "ep_1")

	if err == nil || errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
