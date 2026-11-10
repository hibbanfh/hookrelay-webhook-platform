//go:build integration

// Run with: go test -tags integration ./internal/endpoint/data/repository/...
// The tests need a container runtime (see internal/testsupport/pgtest).

package repositoryimpl_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	repositoryimpl "hookrelay/internal/endpoint/data/repository"
	"hookrelay/internal/endpoint/data/source"
	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/library/random"
	"hookrelay/internal/library/secretbox"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/migrations"
)

const (
	projectA = "proj_A"
	projectB = "proj_B"
)

type harness struct {
	pool    *pgxpool.Pool
	dsn     string
	repo    repository.EndpointRepository
	secrets repository.SecretReader
	tx      *postgres.TxManager
}

func start(t *testing.T) harness {
	t.Helper()
	pool, dsn := pgtest.Start(t)
	box, err := secretbox.New(secretbox.Options{Key: bytes.Repeat([]byte{1}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	src := source.NewEndpointPersistent(pool)
	h := harness{
		pool: pool, dsn: dsn, tx: postgres.NewTxManager(pool),
		repo:    repositoryimpl.NewEndpointRepository(src, box),
		secrets: repositoryimpl.NewSecretReader(src, box),
	}
	h.seedProject(t, projectA)
	h.seedProject(t, projectB)
	return h
}

func (h harness) seedProject(t *testing.T, id string) {
	t.Helper()
	_, err := h.pool.Exec(context.Background(), `INSERT INTO projects (id, name, created_at) VALUES ($1, 'p', now())`, id)
	if err != nil {
		t.Fatal(err)
	}
}

func (h harness) create(t *testing.T, projectID, id string, createdAt time.Time) entity.SigningSecret {
	t.Helper()
	e, err := entity.NewEndpoint(entity.EndpointSpec{ID: id, ProjectID: projectID, URL: "https://example.com/" + id, Description: "d"}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := entity.NewSigningSecret(bytes.Repeat([]byte{byte(len(id))}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.Create(context.Background(), e, secret); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	return secret
}

func epID(n int) string { return fmt.Sprintf("ep_%026d", n) }

var base = time.Date(2026, 11, 9, 12, 0, 0, 123456000, time.UTC)

func TestCreateThenGetAndRevealRoundTrip(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	secret := h.create(t, projectA, epID(1), base)

	got, getErr := h.repo.Get(ctx, projectA, epID(1))
	revealed, secretErr := h.secrets.GetSecret(ctx, projectA, epID(1))

	if getErr != nil || secretErr != nil {
		t.Fatalf("errors: %v %v", getErr, secretErr)
	}
	if got.URL != "https://example.com/"+epID(1) || !got.Enabled || !got.CreatedAt.Equal(base) || !got.UpdatedAt.Equal(base) || got.CreatedAt.Location() != time.UTC {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if revealed.Value() != secret.Value() {
		t.Error("revealed secret differs from the created one")
	}
}

func TestStoredCiphertextIsNotThePlaintextSecret(t *testing.T) {
	h := start(t)
	secret := h.create(t, projectA, epID(1), base)
	var ciphertext []byte
	var version int
	row := h.pool.QueryRow(context.Background(), `SELECT secret_ciphertext, secret_key_version FROM endpoints WHERE id = $1`, epID(1))

	err := row.Scan(&ciphertext, &version)

	plainValue := []byte(secret.Value())
	if err != nil || bytes.Contains(ciphertext, secret.Bytes()) || bytes.Contains(ciphertext, plainValue) || len(ciphertext) <= 28 || version < 1 {
		t.Fatalf("ciphertext %x version %d err %v", ciphertext, version, err)
	}
}

func TestSecretCiphertextCopiedToAnotherRowCannotBeOpened(t *testing.T) {
	h := start(t)
	h.create(t, projectA, epID(1), base)
	h.create(t, projectA, epID(2), base)
	_, err := h.pool.Exec(context.Background(),
		`UPDATE endpoints SET secret_ciphertext = (SELECT secret_ciphertext FROM endpoints WHERE id = $1) WHERE id = $2`, epID(1), epID(2))
	if err != nil {
		t.Fatal(err)
	}

	_, openErr := h.secrets.GetSecret(context.Background(), projectA, epID(2))

	if openErr == nil || errors.Is(openErr, domainerrors.ErrNotFound) {
		t.Fatalf("a swapped ciphertext must fail to open, got %v", openErr)
	}
}

func TestAnotherProjectSeesNotFoundOnGetUpdateAndSecret(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.create(t, projectA, epID(1), base)
	off := false

	_, getErr := h.repo.Get(ctx, projectB, epID(1))
	_, updateErr := h.repo.UpdateFields(ctx, projectB, epID(1), repository.EndpointUpdate{Patch: entity.Patch{Enabled: &off}, At: base})
	_, secretErr := h.secrets.GetSecret(ctx, projectB, epID(1))
	owner, ownerErr := h.repo.Get(ctx, projectA, epID(1))

	assertNotFound(t, "get", getErr)
	assertNotFound(t, "update", updateErr)
	assertNotFound(t, "secret", secretErr)
	if ownerErr != nil || !owner.Enabled {
		t.Errorf("the owner row must be untouched: %+v %v", owner, ownerErr)
	}
}

// listAllIDs walks every page of a project the way a client follows next_cursor.
func (h harness) listAllIDs(t *testing.T, projectID string, pageSize int) []string {
	t.Helper()
	var ids []string
	var after *entity.PageAnchor
	for {
		page, err := h.repo.List(context.Background(), repository.ListQuery{ProjectID: projectID, Limit: pageSize, After: after})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, idsOf(page)...)
		if len(page) < pageSize {
			return ids
		}
		last := page[len(page)-1]
		after = &entity.PageAnchor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

func TestListIsNewestFirstAndKeysetPagesDoNotSkipOrRepeat(t *testing.T) {
	h := start(t)
	// Five endpoints: ids 1-3 share one timestamp (tie broken by id), 4 and 5 are newer.
	times := []time.Time{base, base, base, base.Add(time.Minute), base.Add(2 * time.Minute)}
	for i, at := range times {
		h.create(t, projectA, epID(i+1), at)
	}
	h.create(t, projectB, epID(99), base)

	got := h.listAllIDs(t, projectA, 2)

	want := []string{epID(5), epID(4), epID(3), epID(2), epID(1)}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestConcurrentPatchesOfDifferentFieldsBothSurvive(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.create(t, projectA, epID(1), base)
	newURL, off := "https://changed.example/x", false

	var wg sync.WaitGroup
	for _, p := range []entity.Patch{{URL: &newURL}, {Enabled: &off}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.repo.UpdateFields(ctx, projectA, epID(1), repository.EndpointUpdate{Patch: p, At: base.Add(time.Second)}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	got, err := h.repo.Get(ctx, projectA, epID(1))
	if err != nil || got.URL != newURL || got.Enabled {
		t.Fatalf("both fields must be applied, got %+v, %v", got, err)
	}
}

type allowAllPolicy struct{}

func (allowAllPolicy) Check(context.Context, string) error { return nil }

func (h harness) createUseCase(limit int) *usecase.CreateEndpoint {
	clk := clock.System{}
	return usecase.NewCreateEndpoint(usecase.CreateEndpointDeps{
		Endpoints: h.repo, Policy: allowAllPolicy{}, IDs: ids.NewCryptoGenerator(clk),
		Clock: clk, Random: random.Crypto{}, Tx: h.tx,
	}, limit)
}

// runParallelCreates fires n creates for one project at once and returns how many succeeded
// and how many were refused with the limit error.
func (h harness) runParallelCreates(t *testing.T, projectID string, limit, n int) (created, refused int) {
	t.Helper()
	uc := h.createUseCase(limit)
	start := make(chan struct{})
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			<-start
			_, err := uc.Execute(context.Background(), usecase.CreateEndpointInput{ProjectID: projectID, URL: "https://example.com/hook"})
			results <- err
		}()
	}
	close(start)
	for i := 0; i < n; i++ {
		err := <-results
		if err == nil {
			created++
			continue
		}
		if !errors.Is(err, domainerrors.ErrLimitReached) {
			t.Errorf("unexpected error: %v", err)
		}
		refused++
	}
	return created, refused
}

func (h harness) countEndpoints(t *testing.T, projectID string) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM endpoints WHERE project_id = $1`, projectID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLimitHoldsUnderParallelCreatesAtLimitMinusOne(t *testing.T) {
	const limit, parallel = 5, 10
	h := start(t)
	for i := 1; i < limit; i++ {
		h.create(t, projectA, epID(i), base)
	}

	created, refused := h.runParallelCreates(t, projectA, limit, parallel)

	if created != 1 || refused != parallel-1 || h.countEndpoints(t, projectA) != limit {
		t.Fatalf("created %d, refused %d, stored %d; exactly one create may win", created, refused, h.countEndpoints(t, projectA))
	}
}

func TestLimitHoldsUnderParallelCreatesFromEmpty(t *testing.T) {
	const limit, parallel = 3, 10
	h := start(t)

	created, refused := h.runParallelCreates(t, projectA, limit, parallel)

	if created != limit || refused != parallel-limit || h.countEndpoints(t, projectA) != limit {
		t.Fatalf("created %d, refused %d, stored %d", created, refused, h.countEndpoints(t, projectA))
	}
}

func TestTheLimitIsPerProject(t *testing.T) {
	const limit = 2
	h := start(t)
	h.create(t, projectA, epID(1), base)
	h.create(t, projectA, epID(2), base)

	created, refused := h.runParallelCreates(t, projectB, limit, 3)

	if created != limit || refused != 1 || h.countEndpoints(t, projectA) != limit {
		t.Fatalf("project B created %d, refused %d", created, refused)
	}
}

func TestProjectLockDoesNotBlockInsertsThatReferenceTheProject(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	lockHeld, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- h.tx.WithinTx(ctx, func(ctx context.Context) error {
			if _, err := h.repo.CountForUpdate(ctx, projectA); err != nil {
				return err
			}
			close(lockHeld)
			<-release
			return nil
		})
	}()
	<-lockHeld

	insertCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := h.pool.Exec(insertCtx,
		`INSERT INTO api_keys (id, project_id, key_prefix, key_hash, created_at) VALUES ('key_1', $1, 'abcdefgh', '\x01', now())`, projectA)
	close(release)

	if err != nil || <-done != nil {
		t.Fatalf("an insert that only takes FOR KEY SHARE on the project must not wait for the create lock: %v", err)
	}
}

func TestEndpointsMigrationDownDropsTheTableAndUpRestoresIt(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.create(t, projectA, epID(1), base)

	downErr := postgres.Migrate(h.dsn, migrations.Files, postgres.Down)
	var tableAfterDown *string
	scanDown := h.pool.QueryRow(ctx, `SELECT to_regclass('endpoints')::text`).Scan(&tableAfterDown)
	upErr := postgres.Migrate(h.dsn, migrations.Files, postgres.Up)
	var tableAfterUp *string
	scanUp := h.pool.QueryRow(ctx, `SELECT to_regclass('endpoints')::text`).Scan(&tableAfterUp)

	if downErr != nil || scanDown != nil || tableAfterDown != nil || upErr != nil || scanUp != nil || tableAfterUp == nil {
		t.Fatalf("down %v up %v, table after down %v, after up %v", downErr, upErr, tableAfterDown, tableAfterUp)
	}
}

func assertNotFound(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Errorf("%s from another project: got %v, want not found", name, err)
	}
}

func idsOf(page []entity.Endpoint) []string {
	out := make([]string, 0, len(page))
	for _, e := range page {
		out = append(out, e.ID)
	}
	return out
}

func TestUpdatedAtNeverMovesBackwards(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.create(t, projectA, epID(1), base)
	off := false
	newer := repository.EndpointUpdate{Patch: entity.Patch{Enabled: &off}, At: base.Add(time.Hour)}
	older := repository.EndpointUpdate{Patch: entity.Patch{Description: new(string)}, At: base.Add(time.Minute)}

	_, newerErr := h.repo.UpdateFields(ctx, projectA, epID(1), newer)
	got, olderErr := h.repo.UpdateFields(ctx, projectA, epID(1), older)

	if newerErr != nil || olderErr != nil || !got.UpdatedAt.Equal(newer.At) || got.Description != "" {
		t.Fatalf("updated_at %v must stay at %v: %v %v", got.UpdatedAt, newer.At, newerErr, olderErr)
	}
}
