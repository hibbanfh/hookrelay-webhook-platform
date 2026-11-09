package repositoryimpl_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/project/data/model"
	repositoryimpl "hookrelay/internal/project/data/repository"
	"hookrelay/internal/project/data/source"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

type fakeOwnedSource struct {
	row model.Project
	err error
}

func (f fakeOwnedSource) GetByNeonUserID(context.Context, string) (model.Project, error) {
	return f.row, f.err
}

func TestGetByOwner(t *testing.T) {
	owner := "0b1f9c3e-1111-4222-8333-444455556666"
	row := model.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime, NeonUserID: &owner}
	tests := []struct {
		name  string
		src   fakeOwnedSource
		cause error
		want  outcome
	}{
		{name: "found", src: fakeOwnedSource{row: row}},
		{name: "no row is ErrNotFound", src: fakeOwnedSource{err: source.ErrNotFound}, want: outcome{failed: true, notFound: true}},
		{name: "other errors are wrapped", src: fakeOwnedSource{err: errBoom}, cause: errBoom, want: outcome{failed: true, wrapsCause: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repositoryimpl.NewOwnedProjectRepository(tt.src).GetByOwner(context.Background(), owner)

			if o := classify(err, tt.cause); o != tt.want {
				t.Fatalf("err = %v, got %+v, want %+v", err, o, tt.want)
			}
			if tt.want.failed {
				return
			}
			want := entity.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime.UTC(), OwnerID: owner}
			if got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestOwnerConflictIsErrConflict(t *testing.T) {
	src := &fakeSource{err: source.ErrUniqueViolation}
	p := entity.Project{ID: "proj_1", OwnerID: "user"}

	err := repositoryimpl.NewProjectRepository(src).CreateWithKey(context.Background(), p, entity.APIKey{ID: "key_1"})

	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("got %v", err)
	}
	if src.gotProject.NeonUserID == nil || *src.gotProject.NeonUserID != "user" {
		t.Fatalf("the owner must reach the source: %+v", src.gotProject)
	}
}

func TestUnownedProjectHasNoNeonUserID(t *testing.T) {
	src := &fakeSource{}

	_ = repositoryimpl.NewProjectRepository(src).CreateWithKey(context.Background(), entity.Project{ID: "proj_1"}, entity.APIKey{ID: "key_1"})

	if src.gotProject.NeonUserID != nil {
		t.Fatalf("an unowned project must map to a NULL owner, got %q", *src.gotProject.NeonUserID)
	}
}
