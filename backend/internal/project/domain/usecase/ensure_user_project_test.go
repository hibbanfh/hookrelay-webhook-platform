package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

func TestEnsureUserProject(t *testing.T) {
	winner := entity.Project{ID: "proj_winner", OwnerID: "user-1", Name: entity.DefaultProjectName}
	tests := []struct {
		name        string
		setup       func(*ownerRepo)
		random      fakeRandom
		wantErr     error
		wantID      string
		wantCreated bool
		wantKey     bool
		wantStored  int
	}{
		{name: "first call creates project and key", random: fakeRandom{fill: 7}, wantID: "proj_1", wantCreated: true, wantKey: true, wantStored: 1},
		{name: "existing project is returned without a key", random: fakeRandom{fill: 7}, setup: func(r *ownerRepo) { r.byOwner["user-1"] = winner }, wantID: "proj_winner"},
		{name: "lookup error is wrapped", random: fakeRandom{fill: 7}, setup: func(r *ownerRepo) { r.getErr = errStorage }, wantErr: errStorage},
		{name: "random failure is wrapped", random: fakeRandom{fill: 7, err: errStorage}, wantErr: errStorage},
		{name: "storage failure is wrapped", random: fakeRandom{fill: 7}, setup: func(r *ownerRepo) { r.createErr = errStorage }, wantErr: errStorage},
		{
			name:   "lost the race: returns the winner without a key",
			random: fakeRandom{fill: 7},
			setup:  func(r *ownerRepo) { r.createErr = domainerrors.ErrConflict; r.racer = &winner },
			wantID: "proj_winner",
		},
		{
			name:    "conflict but project still missing",
			random:  fakeRandom{fill: 7},
			setup:   func(r *ownerRepo) { r.createErr = domainerrors.ErrConflict },
			wantErr: domainerrors.ErrConflict,
		},
		{
			name:    "lost the race and re-read fails",
			random:  fakeRandom{fill: 7},
			setup:   func(r *ownerRepo) { r.createErr = domainerrors.ErrConflict; r.getErrAfterCreate = errStorage },
			wantErr: errStorage,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newOwnerRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			deps := usecase.Deps{Projects: repo, IDs: &seqIDs{}, Clock: fixedClock{}, Random: tt.random}

			out, err := usecase.NewEnsureUserProject(deps, repo).Execute(context.Background(), usecase.EnsureUserProjectInput{UserID: "user-1"})

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if out.Project.ID != tt.wantID || out.Created != tt.wantCreated || len(repo.keys) != tt.wantStored {
				t.Fatalf("got %+v created=%v stored=%d", out.Project, out.Created, len(repo.keys))
			}
			if hasKey := out.APIKey.Reveal() != ""; hasKey != tt.wantKey {
				t.Fatalf("raw key returned = %v, want %v", hasKey, tt.wantKey)
			}
		})
	}
}

func TestEnsureUserProjectStoresAnOwnedDefaultProject(t *testing.T) {
	repo := newOwnerRepo()
	deps := usecase.Deps{Projects: repo, IDs: &seqIDs{}, Clock: fixedClock{}, Random: fakeRandom{fill: 3}}

	out, err := usecase.NewEnsureUserProject(deps, repo).Execute(context.Background(), usecase.EnsureUserProjectInput{UserID: "user-1"})

	if err != nil {
		t.Fatal(err)
	}
	stored := repo.byOwner["user-1"]
	if stored.OwnerID != "user-1" || stored.Name != entity.DefaultProjectName || !stored.CreatedAt.Equal(fixedNow) {
		t.Fatalf("stored %+v", stored)
	}
	if !strings.HasPrefix(out.APIKey.Reveal(), "hr_") || repo.keys[0].Hash != out.APIKey.Hash() {
		t.Fatal("the stored key must be the hash of the returned key")
	}
}

func TestEnsureUserProjectRejectsAnEmptyUser(t *testing.T) {
	repo := newOwnerRepo()
	deps := usecase.Deps{Projects: repo, IDs: &seqIDs{}, Clock: fixedClock{}, Random: fakeRandom{fill: 3}}

	_, err := usecase.NewEnsureUserProject(deps, repo).Execute(context.Background(), usecase.EnsureUserProjectInput{})

	if !errors.Is(err, domainerrors.ErrInvalid) || repo.created {
		t.Fatalf("err = %v, created = %v", err, repo.created)
	}
}
