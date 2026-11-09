package usecase_test

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"testing"

	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

func TestSeedDemoProject(t *testing.T) {
	errBoom := errors.New("boom")
	validKey := "hr_" + strings.Repeat("d", 43)
	tests := []struct {
		name            string
		key             string
		hashErr         error
		createErr       error
		winnerOnCreate  bool
		hashErrAfter    error
		nameOverride    string
		wantErr         error
		wantProjectID   string
		wantCreated     bool
		wantStoredCount int
	}{
		{name: "first run creates", key: validKey, wantProjectID: "proj_1", wantCreated: true, wantStoredCount: 1},
		{name: "malformed key", key: "nope", wantErr: domainerrors.ErrInvalid},
		{name: "lookup failure", key: validKey, hashErr: errBoom, wantErr: errBoom},
		{name: "create failure", key: validKey, createErr: errBoom, wantErr: errBoom},
		{
			name: "lost a race: conflict, then the winner's project is returned",
			key:  validKey, createErr: domainerrors.ErrConflict, winnerOnCreate: true,
			wantProjectID: "proj_winner", wantStoredCount: 1,
		},
		{
			name: "conflict but the key is revoked or foreign: clear error",
			key:  validKey, createErr: domainerrors.ErrConflict,
			wantErr: domainerrors.ErrConflict,
		},
		{
			name: "re-read after conflict fails: error is wrapped",
			key:  validKey, createErr: domainerrors.ErrConflict, hashErrAfter: errBoom,
			wantErr: errBoom,
		},
		{name: "invalid name: rejected, nothing stored", key: validKey, nameOverride: " ", wantErr: domainerrors.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.hashErr, repo.createErr, repo.winnerOnCreateErr = tt.hashErr, tt.createErr, tt.winnerOnCreate
			repo.hashErrAfterConflict = tt.hashErrAfter
			uc := usecase.NewSeedDemoProject(newDeps(repo, fakeRandom{}))

			out, err := uc.Execute(context.Background(), usecase.SeedDemoProjectInput{Name: cmp.Or(tt.nameOverride, "Hookrelay Demo"), APIKey: tt.key})

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if out.Project.ID != tt.wantProjectID || out.Created != tt.wantCreated || len(repo.projects) != tt.wantStoredCount {
				t.Fatalf("got project %q created=%v stored=%d, want %q %v %d",
					out.Project.ID, out.Created, len(repo.projects), tt.wantProjectID, tt.wantCreated, tt.wantStoredCount)
			}
		})
	}
}

func TestSeedDemoProjectConflictOnRevokedKeyExplainsWhy(t *testing.T) {
	repo := newFakeRepo()
	repo.createErr = domainerrors.ErrConflict
	uc := usecase.NewSeedDemoProject(newDeps(repo, fakeRandom{}))

	_, err := uc.Execute(context.Background(), usecase.SeedDemoProjectInput{Name: "Demo", APIKey: "hr_" + strings.Repeat("d", 43)})

	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("want an error that mentions the revoked key, got %v", err)
	}
}

func TestSeedDemoProjectIsIdempotentAndKeyAuthenticates(t *testing.T) {
	repo := newFakeRepo()
	uc := usecase.NewSeedDemoProject(newDeps(repo, fakeRandom{}))
	in := usecase.SeedDemoProjectInput{Name: "Hookrelay Demo", APIKey: "hr_" + strings.Repeat("d", 43)}

	first, err1 := uc.Execute(context.Background(), in)
	second, err2 := uc.Execute(context.Background(), in)

	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v %v", err1, err2)
	}
	if !first.Created || second.Created {
		t.Fatalf("Created flags = %v,%v; want true,false", first.Created, second.Created)
	}
	if first.Project.ID != second.Project.ID || len(repo.projects) != 1 || len(repo.keys) != 1 {
		t.Fatalf("second run must not create anything: %d projects, %d keys", len(repo.projects), len(repo.keys))
	}
	id, err := usecase.NewAuthenticateAPIKey(repo).Execute(context.Background(), usecase.AuthenticateAPIKeyInput{Token: in.APIKey})
	if err != nil || id != first.Project.ID {
		t.Fatalf("seeded key must authenticate: %q %v", id, err)
	}
}
