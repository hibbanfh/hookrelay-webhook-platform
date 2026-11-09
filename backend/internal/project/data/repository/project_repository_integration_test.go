//go:build integration

// Run with: go test -tags integration ./internal/project/data/repository/...
// The tests need a container runtime (Docker/Podman). Locally they skip when none is
// reachable; with CI set or REQUIRE_DOCKER=1 a missing runtime fails the run instead, so a
// green CI run always means the database tests really ran.

package repositoryimpl_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	platformpg "hookrelay/internal/platform/postgres"
	repositoryimpl "hookrelay/internal/project/data/repository"
	"hookrelay/internal/project/data/source"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/repository"
	"hookrelay/migrations"
)

const postgresImage = "postgres:16-alpine"

var now = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// dockerRequired is true on CI and when REQUIRE_DOCKER=1: there, no container runtime is a failure.
func dockerRequired() bool {
	return os.Getenv("CI") != "" || os.Getenv("REQUIRE_DOCKER") == "1"
}

func skipOrFail(t *testing.T, err error) {
	t.Helper()
	if dockerRequired() {
		t.Fatalf("cannot start postgres container (a container runtime is required here): %v", err)
	}
	t.Skipf("cannot start postgres container (is a container runtime running?): %v", err)
}

// startDB boots a throwaway Postgres, applies the real migrations and returns a pool.
func startDB(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	if !dockerRequired() {
		testcontainers.SkipIfProviderIsNotHealthy(t)
	}
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("hookrelay"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		skipOrFail(t, err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := platformpg.Migrate(dsn, migrations.Files, platformpg.Up); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := platformpg.Open(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, dsn
}

func newKey(t *testing.T, fill string) entity.RawKey {
	t.Helper()
	k, err := entity.ParseRawKey("hr_" + strings.Repeat(fill, 43))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func seedProject(t *testing.T, repo repository.ProjectRepository, id, name string, key entity.RawKey) {
	t.Helper()
	p := entity.Project{ID: id, Name: name, CreatedAt: now}
	if err := repo.CreateWithKey(context.Background(), p, entity.NewAPIKey("key_"+id, id, key, now)); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

func TestMigrationsUpDownUp(t *testing.T) {
	_, dsn := startDB(t)

	if err := platformpg.Migrate(dsn, migrations.Files, platformpg.Down); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := platformpg.Migrate(dsn, migrations.Files, platformpg.Up); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

func TestCreateAndLookup(t *testing.T) {
	pool, _ := startDB(t)
	repo := newRepo(pool)
	ctx := context.Background()
	keyA, keyB := newKey(t, "a"), newKey(t, "b")
	seedProject(t, repo, "proj_A", "Project A", keyA)
	seedProject(t, repo, "proj_B", "Project B", keyB)

	byID, errID := repo.GetByID(ctx, "proj_A")
	byKeyA, errA := repo.GetByKeyHash(ctx, keyA.Hash())
	byKeyB, errB := repo.GetByKeyHash(ctx, keyB.Hash())
	_, errMissing := repo.GetByID(ctx, "proj_missing")
	_, errUnknown := repo.GetByKeyHash(ctx, newKey(t, "z").Hash())

	if errID != nil || errA != nil || errB != nil {
		t.Fatalf("unexpected errors: %v %v %v", errID, errA, errB)
	}
	if byID.Name != "Project A" || !byID.CreatedAt.Equal(now) {
		t.Errorf("round trip mismatch: %+v", byID)
	}
	if byKeyA.ID != "proj_A" || byKeyB.ID != "proj_B" {
		t.Errorf("keys must resolve to their own project: %s %s", byKeyA.ID, byKeyB.ID)
	}
	if !errors.Is(errMissing, domainerrors.ErrNotFound) || !errors.Is(errUnknown, domainerrors.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v / %v", errMissing, errUnknown)
	}
}

func TestPlaintextKeyIsStoredNowhere(t *testing.T) {
	pool, _ := startDB(t)
	repo := newRepo(pool)
	key := newKey(t, "p")
	seedProject(t, repo, "proj_P", "Plain", key)

	for _, table := range []string{"projects", "api_keys"} {
		var dump string
		q := "SELECT coalesce(string_agg(t::text, ' '), '') FROM " + table + " t"
		if err := pool.QueryRow(context.Background(), q).Scan(&dump); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(dump, key.Reveal()) || strings.Contains(dump, strings.Repeat("p", 43)) {
			t.Fatalf("plaintext key found in %s: %s", table, dump)
		}
	}
	var prefix string
	if err := pool.QueryRow(context.Background(), "SELECT key_prefix FROM api_keys").Scan(&prefix); err != nil || prefix != key.DisplayPrefix() {
		t.Fatalf("prefix = %q, %v", prefix, err)
	}
}

func TestCreateWithKeyConflictsAreAtomic(t *testing.T) {
	pool, _ := startDB(t)
	repo := newRepo(pool)
	ctx := context.Background()
	key := newKey(t, "k")
	seedProject(t, repo, "proj_1", "First", key)

	dupID := repo.CreateWithKey(ctx, entity.Project{ID: "proj_1", Name: "Again", CreatedAt: now},
		entity.NewAPIKey("key_x", "proj_1", newKey(t, "m"), now))
	dupKey := repo.CreateWithKey(ctx, entity.Project{ID: "proj_2", Name: "Second", CreatedAt: now},
		entity.NewAPIKey("key_y", "proj_2", key, now))

	if !errors.Is(dupID, domainerrors.ErrConflict) || !errors.Is(dupKey, domainerrors.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v / %v", dupID, dupKey)
	}
	if _, err := repo.GetByID(ctx, "proj_2"); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("a failed key insert must roll back the project insert, got %v", err)
	}
}

func TestRevokedKeyDoesNotAuthenticate(t *testing.T) {
	pool, _ := startDB(t)
	repo := newRepo(pool)
	key := newKey(t, "r")
	seedProject(t, repo, "proj_R", "Revoked", key)
	if _, err := pool.Exec(context.Background(), "UPDATE api_keys SET revoked_at = now()"); err != nil {
		t.Fatal(err)
	}

	_, err := repo.GetByKeyHash(context.Background(), key.Hash())

	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

// newRepo wires the real source and repository on the container's pool.
func newRepo(pool *pgxpool.Pool) repository.ProjectRepository {
	return repositoryimpl.NewProjectRepository(source.NewProjectPersistent(pool))
}
