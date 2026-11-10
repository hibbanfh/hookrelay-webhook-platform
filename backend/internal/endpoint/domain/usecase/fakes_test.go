package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
)

var fixedNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

var errBoom = errors.New("boom")

type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

type seqIDs struct{ n int }

func (s *seqIDs) New(prefix string) string {
	s.n++
	return fmt.Sprintf("%s%d", prefix, s.n)
}

type fakeRandom struct {
	fill byte
	err  error
}

func (f fakeRandom) Bytes(n int) ([]byte, error) { return bytes.Repeat([]byte{f.fill}, n), f.err }

// recorder remembers the order of calls across fakes, which proves for example that the
// destination policy runs before the transaction opens.
type recorder struct{ calls []string }

func (r *recorder) add(call string) { r.calls = append(r.calls, call) }

type fakePolicy struct {
	rec *recorder
	err error
	got []string
}

func (f *fakePolicy) Check(_ context.Context, rawURL string) error {
	f.rec.add("policy")
	f.got = append(f.got, rawURL)
	return f.err
}

type fakeTx struct {
	rec *recorder
	err error
}

func (f fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.rec.add("tx")
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

// fakeEndpoints is an in-memory repository.EndpointRepository that can be told to fail.
type fakeEndpoints struct {
	rec      *recorder
	rows     []entity.Endpoint
	count    int
	created  []entity.Endpoint
	secrets  []entity.SigningSecret
	gotQuery repository.ListQuery
	gotPatch entity.Patch
	gotAt    time.Time

	countErr, createErr, getErr, listErr, updateErr error
}

func (f *fakeEndpoints) Create(_ context.Context, e entity.Endpoint, s entity.SigningSecret) error {
	f.rec.add("create")
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, e)
	f.secrets = append(f.secrets, s)
	return nil
}

func (f *fakeEndpoints) Get(_ context.Context, projectID, id string) (entity.Endpoint, error) {
	f.rec.add("get")
	if f.getErr != nil {
		return entity.Endpoint{}, f.getErr
	}
	i := slices.IndexFunc(f.rows, func(e entity.Endpoint) bool { return e.ID == id && e.ProjectID == projectID })
	if i < 0 {
		return entity.Endpoint{}, domainerrors.ErrNotFound
	}
	return f.rows[i], nil
}

func (f *fakeEndpoints) List(_ context.Context, q repository.ListQuery) ([]entity.Endpoint, error) {
	f.gotQuery = q
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.rows[:min(len(f.rows), q.Limit)], nil
}

func (f *fakeEndpoints) UpdateFields(_ context.Context, _, id string, u repository.EndpointUpdate) (entity.Endpoint, error) {
	f.rec.add("update")
	f.gotPatch, f.gotAt = u.Patch, u.At
	if f.updateErr != nil {
		return entity.Endpoint{}, f.updateErr
	}
	i := slices.IndexFunc(f.rows, func(e entity.Endpoint) bool { return e.ID == id })
	patched, err := f.rows[i].ApplyPatch(u.Patch, u.At)
	return patched, err
}

func (f *fakeEndpoints) CountForUpdate(context.Context, string) (int, error) {
	f.rec.add("count")
	return f.count, f.countErr
}

// assertError expects wantErr (nil for success) and, for validation errors, the failed field.
func assertError(t *testing.T, err, wantErr error, wantField string) {
	t.Helper()
	var invalid *domainerrors.ValidationError
	gotField := ""
	if errors.As(err, &invalid) {
		gotField = invalid.Field
	}
	if !errors.Is(err, wantErr) || gotField != wantField {
		t.Fatalf("got error %v (field %q), want %v (field %q)", err, gotField, wantErr, wantField)
	}
}

func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
}

func endpointFixture(id, projectID string, createdAt time.Time) entity.Endpoint {
	return entity.Endpoint{
		ID: id, ProjectID: projectID, URL: "https://example.com/hook", Description: "old",
		Enabled: true, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}
