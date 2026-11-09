package source_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hookrelay/internal/project/data/model"
	"hookrelay/internal/project/data/source"
)

const testOwner = "0b1f9c3e-1111-4222-8333-444455556666"

func TestCreateWithKeyPassesTheOwnerAsTheSeventhColumn(t *testing.T) {
	db := &stubDB{}
	owner := testOwner
	p := model.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime, NeonUserID: &owner}
	k := model.APIKey{ID: "key_1", ProjectID: "proj_1", Prefix: "ABCDEFGH", Hash: testHash}

	err := source.NewProjectPersistent(db).CreateWithKey(context.Background(), p, k)

	want := []any{"proj_1", "Acme", testTime, "key_1", "ABCDEFGH", testHash, testOwner}
	if err != nil || !reflect.DeepEqual(db.gotArgs, want) {
		t.Fatalf("err %v, args %#v, want %#v", err, db.gotArgs, want)
	}
}

func TestOwnerUniqueViolationIsReportedAsErrUniqueViolation(t *testing.T) {
	owner := testOwner
	db := &stubDB{execErr: &pgconn.PgError{Code: "23505", ConstraintName: "projects_neon_user_id_key"}}

	err := source.NewProjectPersistent(db).CreateWithKey(context.Background(),
		model.Project{ID: "proj_1", NeonUserID: &owner}, model.APIKey{ID: "key_1"})

	if got := classify(err, nil); !got.conflict {
		t.Fatalf("err = %v, got %+v", err, got)
	}
}

// ownerRowDB returns a four-column project row, the last column being neon_user_id.
type ownerRowDB struct {
	stubDB
	owner *string
}

func (d *ownerRowDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	d.gotArgs = args
	return ownerRow{err: d.rowErr, owner: d.owner}
}

type ownerRow struct {
	err   error
	owner *string
}

func (r ownerRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*string)) = "proj_1"
	*(dest[1].(*string)) = "Acme"
	*(dest[2].(*time.Time)) = testTime
	*(dest[3].(**string)) = r.owner
	return nil
}

func TestGetByNeonUserID(t *testing.T) {
	tests := []struct {
		name   string
		rowErr error
		cause  error
		want   outcome
	}{
		{name: "found"},
		{name: "no rows is not found", rowErr: pgx.ErrNoRows, want: outcome{failed: true, notFound: true}},
		{name: "other errors are wrapped", rowErr: errBoom, cause: errBoom, want: outcome{failed: true, wrapsCause: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := testOwner
			db := &ownerRowDB{stubDB: stubDB{rowErr: tt.rowErr}, owner: &owner}

			got, err := source.NewProjectPersistent(db).GetByNeonUserID(context.Background(), testOwner)

			if o := classify(err, tt.cause); o != tt.want {
				t.Fatalf("err = %v, got %+v, want %+v", err, o, tt.want)
			}
			if !reflect.DeepEqual(db.gotArgs, []any{testOwner}) {
				t.Fatalf("args %#v", db.gotArgs)
			}
			if tt.rowErr == nil && (got.ID != "proj_1" || got.NeonUserID == nil || *got.NeonUserID != testOwner) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestEveryLookupReturnsTheOwnerColumn(t *testing.T) {
	tests := []struct {
		name string
		call func(*source.ProjectPersistentImpl) (model.Project, error)
	}{
		{"by id", lookupByID},
		{"by key hash", lookupByKeyHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := testOwner
			got, err := tt.call(source.NewProjectPersistent(&ownerRowDB{owner: &owner}))

			if err != nil || got.NeonUserID == nil || *got.NeonUserID != testOwner {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestUnownedProjectHasNilOwner(t *testing.T) {
	got, err := source.NewProjectPersistent(&ownerRowDB{}).GetByID(context.Background(), "proj_1")

	if err != nil || got.NeonUserID != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}
