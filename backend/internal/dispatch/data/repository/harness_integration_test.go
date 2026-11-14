//go:build integration

// Run with: go test -tags integration ./internal/dispatch/data/repository/...
// The tests need a container runtime (see internal/testsupport/pgtest).

package repositoryimpl_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/repository"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/testsupport/pgtest"
)

const (
	projectA = "proj_A"
	projectB = "proj_B"
)

var t0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type harness struct {
	pool     *pgxpool.Pool
	dsn      string
	queue    repository.DeliveryQueue
	attempts repository.AttemptRepository
	tx       *postgres.TxManager
}

// start boots a database and seeds two projects with one endpoint each, with plain SQL: dispatch
// tests never import another module.
func start(t *testing.T) harness {
	t.Helper()
	pool, dsn := pgtest.Start(t)
	h := harness{
		pool: pool, dsn: dsn, tx: postgres.NewTxManager(pool),
		queue:    repositoryimpl.NewDeliveryQueue(source.NewDeliveryPersistent(pool), systemClock{}),
		attempts: repositoryimpl.NewAttemptRepository(source.NewAttemptPersistent(pool)),
	}
	h.exec(t, `INSERT INTO projects (id, name, created_at) VALUES ($1, 'a', now()), ($2, 'b', now())`, projectA, projectB)
	h.seedEndpoint(t, projectA, "ep_A")
	h.seedEndpoint(t, projectB, "ep_B")
	return h
}

func (h harness) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

func (h harness) seedEndpoint(t *testing.T, projectID, id string) {
	t.Helper()
	h.exec(t, `INSERT INTO endpoints (id, project_id, url, secret_ciphertext, secret_key_version, created_at, updated_at)
		VALUES ($1, $2, 'https://example.com/hook', decode(repeat('00', 40), 'hex'), 1, now(), now())`, id, projectID)
}

// seedDeliveries inserts n pending deliveries of endpoint ep_A, due one millisecond apart from
// first, each with its own message (one delivery per message and endpoint).
func (h harness) seedDeliveries(t *testing.T, n int, first time.Time) {
	t.Helper()
	h.exec(t, `INSERT INTO messages (id, project_id, event_type, payload, created_at)
		SELECT 'msg_' || lpad(i::text, 6, '0'), $1, 'e', '{}', now() FROM generate_series(1, $2::int) AS i`, projectA, n)
	h.exec(t, `INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, next_attempt_at, created_at, updated_at)
		SELECT 'dlv_' || lpad(i::text, 6, '0'), $1, 'msg_' || lpad(i::text, 6, '0'), 'ep_A', 'pending',
		       $3::timestamptz + i * interval '1 millisecond', now(), now()
		FROM generate_series(1, $2::int) AS i`, projectA, n, first)
}

// seedOne inserts one delivery with explicit state, for the cases that need a particular row.
func (h harness) seedOne(t *testing.T, id, projectID, endpointID, status string, nextAt *time.Time) {
	t.Helper()
	msg := "msg_" + id
	h.exec(t, `INSERT INTO messages (id, project_id, event_type, payload, created_at) VALUES ($1, $2, 'e', '{}', now())`, msg, projectID)
	h.exec(t, `INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, next_attempt_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())`, id, projectID, msg, endpointID, status, nextAt)
}

func (h harness) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
