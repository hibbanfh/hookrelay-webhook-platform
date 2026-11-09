//go:build integration

package repositoryimpl_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/platform/clock"
	"hookrelay/internal/platform/ids"
	"hookrelay/internal/platform/random"
	repositoryimpl "hookrelay/internal/project/data/repository"
	"hookrelay/internal/project/data/source"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

const (
	ownerA = "0b1f9c3e-aaaa-4222-8333-444455556666"
	ownerB = "0b1f9c3e-bbbb-4222-8333-444455556666"
)

func newEnsure(pool *pgxpool.Pool) *usecase.EnsureUserProject {
	src := source.NewProjectPersistent(pool)
	deps := usecase.Deps{
		Projects: repositoryimpl.NewProjectRepository(src),
		IDs:      ids.NewGenerator(clock.System{}, ids.CryptoEntropy),
		Clock:    clock.System{},
		Random:   random.Crypto{},
	}
	return usecase.NewEnsureUserProject(deps, repositoryimpl.NewOwnedProjectRepository(src))
}

func TestUnownedProjectsCoexistAndStillAuthenticateByKey(t *testing.T) {
	pool, _ := startDB(t)
	repo := newRepo(pool)
	keyA, keyB := newKey(t, "a"), newKey(t, "b")
	seedProject(t, repo, "proj_A", "Seeded demo", keyA)
	seedProject(t, repo, "proj_B", "Another", keyB)

	got, err := repo.GetByKeyHash(context.Background(), keyA.Hash())

	if err != nil || got.ID != "proj_A" || got.OwnerID != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOwnedProjectRoundTripAndOwnerUniqueness(t *testing.T) {
	pool, _ := startDB(t)
	repo := newRepo(pool)
	owners := repositoryimpl.NewOwnedProjectRepository(source.NewProjectPersistent(pool))
	ctx := context.Background()
	p := entity.Project{ID: "proj_1", Name: "Mine", CreatedAt: now, OwnerID: ownerA}
	if err := repo.CreateWithKey(ctx, p, entity.NewAPIKey("key_1", "proj_1", newKey(t, "a"), now)); err != nil {
		t.Fatal(err)
	}
	byID, errID := repo.GetByID(ctx, "proj_1")
	byKey, errKey := repo.GetByKeyHash(ctx, newKey(t, "a").Hash())
	if errID != nil || errKey != nil || byID.OwnerID != ownerA || byKey.OwnerID != ownerA {
		t.Fatalf("an owned project loaded by id/key must carry its owner: %+v (%v) %+v (%v)", byID, errID, byKey, errKey)
	}
	second := entity.Project{ID: "proj_2", Name: "Second", CreatedAt: now, OwnerID: ownerA}

	dup := repo.CreateWithKey(ctx, second, entity.NewAPIKey("key_2", "proj_2", newKey(t, "b"), now))
	got, err := owners.GetByOwner(ctx, ownerA)

	if !errors.Is(dup, domainerrors.ErrConflict) {
		t.Fatalf("a second project for the same owner must conflict, got %v", dup)
	}
	if err != nil || got.ID != "proj_1" || got.OwnerID != ownerA {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := repo.GetByID(ctx, "proj_2"); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("the rejected project must not exist, got %v", err)
	}
}

func TestGetByOwnerKeepsUsersApart(t *testing.T) {
	pool, _ := startDB(t)
	repo := newRepo(pool)
	owners := repositoryimpl.NewOwnedProjectRepository(source.NewProjectPersistent(pool))
	ctx := context.Background()
	for i, o := range []string{ownerA, ownerB} {
		id := []string{"proj_A", "proj_B"}[i]
		p := entity.Project{ID: id, Name: id, CreatedAt: now, OwnerID: o}
		if err := repo.CreateWithKey(ctx, p, entity.NewAPIKey("key_"+id, id, newKey(t, string(rune('a'+i))), now)); err != nil {
			t.Fatal(err)
		}
	}

	a, errA := owners.GetByOwner(ctx, ownerA)
	b, errB := owners.GetByOwner(ctx, ownerB)
	_, errC := owners.GetByOwner(ctx, "0b1f9c3e-cccc-4222-8333-444455556666")

	if errA != nil || errB != nil || a.ID != "proj_A" || b.ID != "proj_B" {
		t.Fatalf("a=%+v (%v) b=%+v (%v)", a, errA, b, errB)
	}
	if !errors.Is(errC, domainerrors.ErrNotFound) {
		t.Fatalf("an unknown user must have no project, got %v", errC)
	}
}

func TestConcurrentEnsureCreatesExactlyOneProject(t *testing.T) {
	const callers = 10
	pool, _ := startDB(t)
	ensure := newEnsure(pool)
	var created atomic.Int64
	projectIDs := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := ensure.Execute(context.Background(), usecase.EnsureUserProjectInput{UserID: ownerA})
			if out.Created {
				created.Add(1)
			}
			projectIDs <- out.Project.ID
			errs <- err
		}()
	}
	wg.Wait()
	close(projectIDs)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("ensure failed: %v", err)
		}
	}
	distinct := map[string]struct{}{}
	for id := range projectIDs {
		distinct[id] = struct{}{}
	}
	var rows, keys int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM projects WHERE neon_user_id = $1::uuid", ownerA).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM api_keys").Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if created.Load() != 1 || len(distinct) != 1 || rows != 1 || keys != 1 {
		t.Fatalf("created=%d distinct ids=%d project rows=%d key rows=%d, want 1 each", created.Load(), len(distinct), rows, keys)
	}
}
