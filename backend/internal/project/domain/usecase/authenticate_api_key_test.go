package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

// issuedKey is the key a fakeRandom{fill: 1} source produces, so tables can name it up front.
func issuedKey(t *testing.T, fill byte) entity.RawKey {
	t.Helper()
	key, err := entity.NewRawKey(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// provisionProject stores a project whose key is issuedKey(t, fill) and returns its ID.
// Share deps between calls so project IDs keep counting up.
func provisionProject(t *testing.T, deps usecase.Deps, name string, fill byte) string {
	t.Helper()
	deps.Random = fakeRandom{fill: fill}
	out, err := usecase.NewCreateProject(deps, true).
		Execute(context.Background(), usecase.CreateProjectInput{Name: name})
	if err != nil {
		t.Fatalf("provision %s: %v", name, err)
	}
	return out.Project.ID
}

func TestAuthenticateAPIKey(t *testing.T) {
	errBoom := errors.New("db down")
	issued := issuedKey(t, 1).Reveal()
	tests := []struct {
		name    string
		token   string
		hashErr error
		wantID  string
		wantErr error
	}{
		{name: "valid key", token: issued, wantID: "proj_1"},
		{name: "empty token", token: "", wantErr: domainerrors.ErrUnauthorized},
		{name: "malformed token", token: "not-a-key", wantErr: domainerrors.ErrUnauthorized},
		{name: "unknown key", token: "hr_" + strings.Repeat("z", 43), wantErr: domainerrors.ErrUnauthorized},
		{name: "storage failure is not masked as 401", token: issued, hashErr: errBoom, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			provisionProject(t, newDeps(repo, fakeRandom{}), "A", 1)
			repo.hashErr = tt.hashErr

			id, err := usecase.NewAuthenticateAPIKey(repo).Execute(context.Background(), usecase.AuthenticateAPIKeyInput{Token: tt.token})

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if id != tt.wantID {
				t.Fatalf("project id = %q, want %q", id, tt.wantID)
			}
		})
	}
}

func TestAuthenticateAPIKeyIsolatesProjects(t *testing.T) {
	repo := newFakeRepo()
	deps := newDeps(repo, fakeRandom{})
	idA := provisionProject(t, deps, "A", 1)
	idB := provisionProject(t, deps, "B", 2)
	uc := usecase.NewAuthenticateAPIKey(repo)

	gotA, errA := uc.Execute(context.Background(), usecase.AuthenticateAPIKeyInput{Token: issuedKey(t, 1).Reveal()})
	gotB, errB := uc.Execute(context.Background(), usecase.AuthenticateAPIKeyInput{Token: issuedKey(t, 2).Reveal()})

	if errA != nil || errB != nil || gotA != idA || gotB != idB || gotA == gotB {
		t.Fatalf("keys must resolve to their own project: %q %q %v %v", gotA, gotB, errA, errB)
	}
}
