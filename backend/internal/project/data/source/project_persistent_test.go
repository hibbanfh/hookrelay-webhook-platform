package source_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hookrelay/internal/project/data/model"
	"hookrelay/internal/project/data/source"
)

// stubDB exercises the repository without a database and records what it was asked to run.
type stubDB struct {
	execErr error
	rowErr  error
	row     [3]any
	gotArgs []any
}

func (s *stubDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	s.gotArgs = args
	return pgconn.CommandTag{}, s.execErr
}

func (s *stubDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	s.gotArgs = args
	return stubRow{err: s.rowErr, vals: s.row}
}

type stubRow struct {
	err  error
	vals [3]any
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*string)) = r.vals[0].(string)
	*(dest[1].(*string)) = r.vals[1].(string)
	*(dest[2].(*time.Time)) = r.vals[2].(time.Time)
	return nil
}

// outcome is the classification of an error that tests compare as one value.
type outcome struct{ failed, conflict, notFound, wrapsCause bool }

func classify(err, cause error) outcome {
	return outcome{
		failed:     err != nil,
		conflict:   errors.Is(err, source.ErrUniqueViolation),
		notFound:   errors.Is(err, source.ErrNotFound),
		wrapsCause: cause != nil && errors.Is(err, cause),
	}
}

var (
	errBoom      = errors.New("boom")
	errPgConnect = &pgconn.PgError{Code: "08006"}
	testHash     = []byte{1, 2, 3}
	testTime     = time.Date(2026, 11, 8, 12, 0, 0, 0, time.FixedZone("x", 3600))
)

func TestCreateWithKeyPassesEveryColumnInOrder(t *testing.T) {
	db := &stubDB{}
	p := model.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime}
	k := model.APIKey{ID: "key_1", ProjectID: "proj_1", Prefix: "ABCDEFGH", Hash: testHash}

	err := source.NewProjectPersistent(db).CreateWithKey(context.Background(), p, k)

	want := []any{"proj_1", "Acme", testTime, "key_1", "ABCDEFGH", testHash}
	if err != nil || !reflect.DeepEqual(db.gotArgs, want) {
		t.Fatalf("err %v, args %#v, want %#v", err, db.gotArgs, want)
	}
}

func TestCreateWithKeyTranslatesStorageErrors(t *testing.T) {
	tests := []struct {
		name    string
		execErr error
		cause   error
		want    outcome
	}{
		{name: "ok"},
		{name: "unique violation is reported as ErrUniqueViolation", execErr: &pgconn.PgError{Code: "23505"}, want: outcome{failed: true, conflict: true}},
		{name: "other pg error is wrapped, not mapped", execErr: errPgConnect, cause: errPgConnect, want: outcome{failed: true, wrapsCause: true}},
		{name: "unknown error is wrapped", execErr: errBoom, cause: errBoom, want: outcome{failed: true, wrapsCause: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := source.NewProjectPersistent(&stubDB{execErr: tt.execErr})

			err := repo.CreateWithKey(context.Background(), model.Project{ID: "proj_1"}, model.APIKey{ID: "key_1"})

			if got := classify(err, tt.cause); got != tt.want {
				t.Fatalf("err = %v, got %+v, want %+v", err, got, tt.want)
			}
		})
	}
}

func lookupByID(r *source.ProjectPersistentImpl) (model.Project, error) {
	return r.GetByID(context.Background(), "proj_1")
}

func lookupByKeyHash(r *source.ProjectPersistentImpl) (model.Project, error) {
	return r.GetByKeyHash(context.Background(), testHash)
}

func TestLookupsPassTheKeyAndReturnTheRow(t *testing.T) {
	tests := []struct {
		name     string
		call     func(*source.ProjectPersistentImpl) (model.Project, error)
		wantArgs []any
	}{
		{"by id", lookupByID, []any{"proj_1"}},
		{"by key hash", lookupByKeyHash, []any{testHash}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &stubDB{row: [3]any{"proj_1", "Acme", testTime}}

			got, err := tt.call(source.NewProjectPersistent(db))

			want := model.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime}
			if err != nil || got != want {
				t.Fatalf("got %+v (%v), want %+v", got, err, want)
			}
			if !reflect.DeepEqual(db.gotArgs, tt.wantArgs) {
				t.Fatalf("args %#v, want %#v", db.gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestLookupsTranslateStorageErrors(t *testing.T) {
	tests := []struct {
		name   string
		call   func(*source.ProjectPersistentImpl) (model.Project, error)
		rowErr error
		cause  error
		want   outcome
	}{
		{"by id: no rows is not found", lookupByID, pgx.ErrNoRows, nil, outcome{failed: true, notFound: true}},
		{"by id: other errors are wrapped, never not found", lookupByID, errBoom, errBoom, outcome{failed: true, wrapsCause: true}},
		{"by key hash: no rows is not found", lookupByKeyHash, pgx.ErrNoRows, nil, outcome{failed: true, notFound: true}},
		{"by key hash: other errors are wrapped, never not found", lookupByKeyHash, errBoom, errBoom, outcome{failed: true, wrapsCause: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := source.NewProjectPersistent(&stubDB{rowErr: tt.rowErr})

			_, err := tt.call(repo)

			if got := classify(err, tt.cause); got != tt.want {
				t.Fatalf("err = %v, got %+v, want %+v", err, got, tt.want)
			}
		})
	}
}
