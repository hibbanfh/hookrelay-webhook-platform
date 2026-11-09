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

func newDeps(repo *fakeRepo, rnd fakeRandom) usecase.Deps {
	return usecase.Deps{Projects: repo, IDs: &seqIDs{}, Clock: fixedClock{}, Random: rnd}
}

func TestCreateProject(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name      string
		enabled   bool
		inName    string
		repoErr   error
		randomErr error
		wantErr   error
		wantStore bool
	}{
		{name: "success", enabled: true, inName: "Acme", wantStore: true},
		{name: "name is trimmed", enabled: true, inName: "  Acme ", wantStore: true},
		{name: "creation disabled", enabled: false, inName: "Acme", wantErr: domainerrors.ErrCreationDisabled},
		{name: "empty name", enabled: true, inName: "", wantErr: domainerrors.ErrInvalid},
		{name: "whitespace name", enabled: true, inName: "   ", wantErr: domainerrors.ErrInvalid},
		{name: "101 chars", enabled: true, inName: strings.Repeat("a", 101), wantErr: domainerrors.ErrInvalid},
		{name: "NUL in name", enabled: true, inName: "a\x00b", wantErr: domainerrors.ErrInvalid},
		{name: "control character in name", enabled: true, inName: "a\x07b", wantErr: domainerrors.ErrInvalid},
		{name: "random source fails", enabled: true, inName: "Acme", randomErr: errBoom, wantErr: errBoom},
		{name: "repository fails", enabled: true, inName: "Acme", repoErr: errBoom, wantErr: errBoom},
		{name: "repository conflict", enabled: true, inName: "Acme", repoErr: domainerrors.ErrConflict, wantErr: domainerrors.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.createErr = tt.repoErr
			uc := usecase.NewCreateProject(newDeps(repo, fakeRandom{fill: 9, err: tt.randomErr}), tt.enabled)

			out, err := uc.Execute(context.Background(), usecase.CreateProjectInput{Name: tt.inName})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got %v, want %v", err, tt.wantErr)
				}
				if len(repo.projects) != 0 {
					t.Fatal("nothing may be stored on failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Project.ID != "proj_1" || out.Project.Name != "Acme" || !out.Project.CreatedAt.Equal(fixedNow) {
				t.Errorf("unexpected project %+v", out.Project)
			}
		})
	}
}

func TestCreateProjectStoresOnlyTheHashOfTheKey(t *testing.T) {
	repo := newFakeRepo()
	uc := usecase.NewCreateProject(newDeps(repo, fakeRandom{fill: 9}), true)

	out, err := uc.Execute(context.Background(), usecase.CreateProjectInput{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	raw := out.APIKey.Reveal()
	if _, err := entity.ParseRawKey(raw); err != nil {
		t.Fatalf("returned key has a bad format: %v", err)
	}
	if len(repo.keys) != 1 {
		t.Fatalf("want one stored key, got %d", len(repo.keys))
	}
	stored := repo.keys[0]
	if stored.Hash != out.APIKey.Hash() || stored.ProjectID != out.Project.ID {
		t.Errorf("stored key does not match: %+v", stored)
	}
	if stored.Prefix != raw[3:11] {
		t.Errorf("prefix %q must be the first 8 chars after hr_", stored.Prefix)
	}
}

func TestCreateProjectCheckEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		wantErr error
	}{
		{"enabled", true, nil},
		{"disabled", false, domainerrors.ErrCreationDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := usecase.NewCreateProject(newDeps(newFakeRepo(), fakeRandom{}), tt.enabled)

			if err := uc.CheckEnabled(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}
