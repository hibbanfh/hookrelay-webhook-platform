//go:build integration

// Run with: go test -tags integration ./internal/message/data/repository/...
// The tests need a container runtime (see internal/testsupport/pgtest).

package repositoryimpl_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	repositoryimpl "hookrelay/internal/message/data/repository"
	"hookrelay/internal/message/data/source"
	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/repository"
	"hookrelay/internal/testsupport/pgtest"
)

const (
	projectA = "proj_A"
	projectB = "proj_B"
)

type harness struct {
	pool      *pgxpool.Pool
	messages  repository.MessageRepository
	keys      repository.IdempotencyStore
	directory repository.EndpointDirectory
	outbox    repository.DeliveryOutbox
}

func start(t *testing.T) harness {
	t.Helper()
	pool, _ := pgtest.Start(t)
	fanout := source.NewFanoutPersistent(pool)
	h := harness{
		pool:      pool,
		messages:  repositoryimpl.NewMessageRepository(source.NewMessagePersistent(pool)),
		keys:      repositoryimpl.NewIdempotencyStore(source.NewIdempotencyPersistent(pool)),
		directory: repositoryimpl.NewEndpointDirectory(fanout),
		outbox:    repositoryimpl.NewDeliveryOutbox(fanout),
	}
	h.exec(t, `INSERT INTO projects (id, name, created_at) VALUES ($1, 'a', now()), ($2, 'b', now())`, projectA, projectB)
	return h
}

func (h harness) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

// seedEndpoint inserts an endpoint with direct SQL: the message module must not import endpoint.
func (h harness) seedEndpoint(t *testing.T, projectID, id string, enabled bool) {
	t.Helper()
	h.exec(t, `INSERT INTO endpoints (id, project_id, url, enabled, secret_ciphertext, secret_key_version, created_at, updated_at)
		VALUES ($1, $2, 'https://example.com/hook', $3, decode(repeat('00', 40), 'hex'), 1, now(), now())`, id, projectID, enabled)
}

func (h harness) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var t0 = time.Date(2026, 11, 13, 9, 30, 0, 123456000, time.UTC)

func newMessage(t *testing.T, projectID, id string, payload []byte) entity.Message {
	t.Helper()
	m, err := entity.NewMessage(entity.MessageSpec{ID: id, ProjectID: projectID, EventType: "invoice.paid", Payload: payload}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (h harness) storeMessage(t *testing.T, projectID, id string) entity.Message {
	t.Helper()
	m := newMessage(t, projectID, id, []byte(`{}`))
	if err := h.messages.Create(context.Background(), m); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	return m
}

func epID(n int) string { return fmt.Sprintf("ep_%026d", n) }

func TestPayloadRoundTripsByteForByte(t *testing.T) {
	h := start(t)
	payloads := [][]byte{
		[]byte(`{"b":1,  "a":[1,2], "a":3}`),
		[]byte("{\"name\":\"Zo\u00eb \u4f60\u597d \\u00e9\",\n\t\"x\" : 1.0e2}"),
	}
	for i, payload := range payloads {
		id := fmt.Sprintf("msg_%d", i)
		if err := h.messages.Create(context.Background(), newMessage(t, projectA, id, payload)); err != nil {
			t.Fatal(err)
		}

		got, err := h.messages.Get(context.Background(), projectA, id)

		if err != nil || string(got.Payload) != string(payload) {
			t.Fatalf("payload %q came back as %q (%v)", payload, got.Payload, err)
		}
		if !got.CreatedAt.Equal(t0) || got.CreatedAt.Location() != time.UTC || got.EventType != "invoice.paid" {
			t.Errorf("round trip mismatch: %+v", got)
		}
	}
}

func TestGetIsScopedToTheProject(t *testing.T) {
	h := start(t)
	h.storeMessage(t, projectA, "msg_1")

	_, foreign := h.messages.Get(context.Background(), projectB, "msg_1")
	_, missing := h.messages.Get(context.Background(), projectA, "msg_missing")

	if !errors.Is(foreign, domainerrors.ErrNotFound) || !errors.Is(missing, domainerrors.ErrNotFound) {
		t.Fatalf("foreign %v, missing %v", foreign, missing)
	}
}

func TestEnabledEndpointIDsReturnsOnlyEnabledEndpointsOfTheProject(t *testing.T) {
	h := start(t)
	h.seedEndpoint(t, projectA, epID(3), true)
	h.seedEndpoint(t, projectA, epID(1), true)
	h.seedEndpoint(t, projectA, epID(2), false)
	h.seedEndpoint(t, projectB, epID(4), true)

	got, err := h.directory.EnabledEndpointIDs(context.Background(), projectA)

	if err != nil || len(got) != 2 || got[0] != epID(1) || got[1] != epID(3) {
		t.Fatalf("got %v %v, want the two enabled endpoints of A in ID order", got, err)
	}
	none, err := h.directory.EnabledEndpointIDs(context.Background(), "proj_nobody")
	if err != nil || len(none) != 0 {
		t.Fatalf("a project without endpoints: %v %v", none, err)
	}
}

func record(m entity.Message, key string, createdAt time.Time, ttl time.Duration) entity.IdempotencyRecord {
	m.CreatedAt = createdAt
	return entity.NewIdempotencyRecord(m, entity.IdempotencyKey(key), ttl)
}

func TestReserveLiveKeyIsHeldUntilExpiry(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	first := h.storeMessage(t, projectA, "msg_1")
	second := h.storeMessage(t, projectA, "msg_2")
	third := h.storeMessage(t, projectA, "msg_3")
	expiresAt := t0.Add(time.Hour)

	won1, err1 := h.keys.Reserve(ctx, record(first, "k", t0, time.Hour))
	won2, err2 := h.keys.Reserve(ctx, record(second, "k", expiresAt.Add(-time.Microsecond), time.Hour))
	beforeTakeover, errFind := h.keys.Find(ctx, projectA, "k")
	won3, err3 := h.keys.Reserve(ctx, record(third, "k", expiresAt, time.Hour))
	afterTakeover, errFind2 := h.keys.Find(ctx, projectA, "k")

	if err1 != nil || err2 != nil || err3 != nil || errFind != nil || errFind2 != nil {
		t.Fatalf("errors: %v %v %v %v %v", err1, err2, err3, errFind, errFind2)
	}
	if !won1 || won2 || !won3 {
		t.Fatalf("won = %v %v %v, want true false true (the boundary is inclusive)", won1, won2, won3)
	}
	if beforeTakeover.MessageID != "msg_1" || afterTakeover.MessageID != "msg_3" || !afterTakeover.ExpiresAt.Equal(expiresAt.Add(time.Hour)) {
		t.Errorf("message ids %s then %s, expires %v", beforeTakeover.MessageID, afterTakeover.MessageID, afterTakeover.ExpiresAt)
	}
}

func TestReserveStoresTheFingerprintAndFindReadsItBack(t *testing.T) {
	h := start(t)
	m := h.storeMessage(t, projectA, "msg_1")
	rec := record(m, "k", t0, time.Hour)
	if _, err := h.keys.Reserve(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	got, err := h.keys.Find(context.Background(), projectA, "k")

	if err != nil || got != rec {
		t.Fatalf("got %+v %v, want %+v", got, err, rec)
	}
	_, missing := h.keys.Find(context.Background(), projectA, "other")
	if !errors.Is(missing, domainerrors.ErrNotFound) {
		t.Fatalf("missing key: %v", missing)
	}
}

func TestReserveScopesKeysPerProject(t *testing.T) {
	h := start(t)
	a := h.storeMessage(t, projectA, "msg_a")
	b := h.storeMessage(t, projectB, "msg_b")

	wonA, errA := h.keys.Reserve(context.Background(), record(a, "same-key", t0, time.Hour))
	wonB, errB := h.keys.Reserve(context.Background(), record(b, "same-key", t0, time.Hour))

	if errA != nil || errB != nil || !wonA || !wonB {
		t.Fatalf("A %v %v, B %v %v: the same key in two projects must be reserved twice", wonA, errA, wonB, errB)
	}
}

func TestEnqueueWritesDeliveriesInTheirInitialState(t *testing.T) {
	h := start(t)
	m := h.storeMessage(t, projectA, "msg_1")
	for i := 1; i <= 20; i++ {
		h.seedEndpoint(t, projectA, epID(i), true)
	}
	deliveries := make([]entity.PendingDelivery, 0, 20)
	for i := 1; i <= 20; i++ {
		deliveries = append(deliveries, entity.NewPendingDelivery(fmt.Sprintf("dlv_%02d", i), m, epID(i)))
	}

	err := h.outbox.Enqueue(context.Background(), deliveries)

	if err != nil {
		t.Fatal(err)
	}
	pending := h.count(t, `SELECT count(*) FROM deliveries WHERE message_id = 'msg_1' AND project_id = $1 AND status = 'pending'
		AND trigger = 'automatic' AND attempt_count = 0 AND next_attempt_at = created_at AND updated_at = created_at
		AND created_at = $2 AND lease_token IS NULL AND last_attempt_at IS NULL`, projectA, t0)
	if pending != 20 {
		t.Fatalf("%d of 20 deliveries are in the initial state", pending)
	}
}

func TestDatabaseRefusesADeliveryAcrossProjects(t *testing.T) {
	h := start(t)
	messageA := h.storeMessage(t, projectA, "msg_a")
	h.seedEndpoint(t, projectB, epID(1), true)
	crossed := entity.NewPendingDelivery("dlv_1", messageA, epID(1)) // A's message, B's endpoint
	crossed.ProjectID = projectB                                     // B's project, A's message
	wrongProject := entity.NewPendingDelivery("dlv_2", messageA, epID(1))

	errMessage := h.outbox.Enqueue(context.Background(), []entity.PendingDelivery{crossed})
	errEndpoint := h.outbox.Enqueue(context.Background(), []entity.PendingDelivery{wrongProject})

	if errMessage == nil || errEndpoint == nil || h.count(t, `SELECT count(*) FROM deliveries`) != 0 {
		t.Fatalf("composite foreign keys must refuse both: %v / %v", errMessage, errEndpoint)
	}
}
