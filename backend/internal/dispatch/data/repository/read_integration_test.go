//go:build integration

package repositoryimpl_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
)

func (h harness) readers() (repository.DeliveryReader, repository.AttemptReader) {
	return repositoryimpl.NewDeliveryReader(source.NewDeliveryReadPersistent(h.pool)),
		repositoryimpl.NewAttemptReader(source.NewAttemptPersistent(h.pool))
}

// seedTied inserts n pending deliveries of ep_A that all share created_at, one message each, so
// only the ID can order them.
func (h harness) seedTied(t *testing.T, n int, createdAt time.Time) {
	t.Helper()
	h.exec(t, `INSERT INTO messages (id, project_id, event_type, payload, created_at)
		SELECT 'msg_t' || lpad(i::text, 5, '0'), $1, 'e', '{}', $3 FROM generate_series(1, $2::int) AS i`, projectA, n, createdAt)
	h.exec(t, `INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, next_attempt_at, created_at, updated_at)
		SELECT 'dlv_t' || lpad(i::text, 5, '0'), $1, 'msg_t' || lpad(i::text, 5, '0'), 'ep_A', 'pending', $3, $3, $3
		FROM generate_series(1, $2::int) AS i`, projectA, n, createdAt)
}

// seedStatus inserts one delivery of ep_A in the given status at createdAt.
func (h harness) seedStatus(t *testing.T, id, status string, createdAt time.Time) {
	t.Helper()
	var next *time.Time
	if status == "pending" {
		next = &createdAt
	}
	h.exec(t, `INSERT INTO messages (id, project_id, event_type, payload, created_at) VALUES ($1, $2, 'e', '{}', $3)`, "msg_"+id, projectA, createdAt)
	h.exec(t, `INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, next_attempt_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'ep_A', $4, $5, $6, $6)`, id, projectA, "msg_"+id, status, next, createdAt)
}

func ids(ds []entity.Delivery) string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return strings.Join(out, ",")
}

func TestDeliveryGetIsProjectScoped(t *testing.T) {
	h := start(t)
	due := t0
	h.seedOne(t, "dlv_1", projectA, "ep_A", "pending", &due)
	deliveries, _ := h.readers()
	ctx := context.Background()

	got, err := deliveries.Get(ctx, projectA, "dlv_1")

	if err != nil || got.ID != "dlv_1" || got.MessageID != "msg_dlv_1" || got.EndpointID != "ep_A" || got.Status != entity.StatusPending ||
		got.Trigger != entity.TriggerAutomatic || got.AttemptCount != 0 || got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(due) ||
		got.LastAttemptAt != nil || got.CreatedAt.IsZero() || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("delivery %+v, err %v", got, err)
	}
	_, foreign := deliveries.Get(ctx, projectB, "dlv_1")
	_, unknown := deliveries.Get(ctx, projectA, "dlv_nope")
	for name, err := range map[string]error{"foreign project": foreign, "unknown id": unknown} {
		if !errors.Is(err, domainerrors.ErrDeliveryNotFound) || !errors.Is(err, domainerrors.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrDeliveryNotFound", name, err)
		}
	}
}

func TestListByEndpointPagesWithTiedCreatedAtOrderedByID(t *testing.T) {
	h := start(t)
	h.seedTied(t, 5, t0)
	deliveries, _ := h.readers()
	ctx := context.Background()
	q := repository.EndpointDeliveriesQuery{ProjectID: projectA, EndpointID: "ep_A", Limit: 2}

	first, err1 := deliveries.ListByEndpoint(ctx, q)
	q.After = &entity.PageAnchor{CreatedAt: first[1].CreatedAt, ID: first[1].ID}
	second, err2 := deliveries.ListByEndpoint(ctx, q)
	q.After = &entity.PageAnchor{CreatedAt: second[1].CreatedAt, ID: second[1].ID}
	third, err3 := deliveries.ListByEndpoint(ctx, q)

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("errors: %v %v %v", err1, err2, err3)
	}
	if ids(first) != "dlv_t00005,dlv_t00004" || ids(second) != "dlv_t00003,dlv_t00002" || ids(third) != "dlv_t00001" {
		t.Fatalf("pages = %q | %q | %q", ids(first), ids(second), ids(third))
	}
}

func TestListByEndpointNewestFirstAcrossDifferentTimes(t *testing.T) {
	h := start(t)
	h.seedStatus(t, "dlv_old", "failed", t0)
	h.seedStatus(t, "dlv_new", "failed", t0.Add(time.Hour))
	deliveries, _ := h.readers()

	got, err := deliveries.ListByEndpoint(context.Background(), repository.EndpointDeliveriesQuery{ProjectID: projectA, EndpointID: "ep_A", Limit: 10})

	if err != nil || ids(got) != "dlv_new,dlv_old" {
		t.Fatalf("got %q, err %v", ids(got), err)
	}
}

func TestListByEndpointFiltersByStatus(t *testing.T) {
	h := start(t)
	h.seedStatus(t, "dlv_p", "pending", t0)
	h.seedStatus(t, "dlv_s", "succeeded", t0.Add(time.Minute))
	h.seedStatus(t, "dlv_f1", "failed", t0.Add(2*time.Minute))
	h.seedStatus(t, "dlv_f2", "failed", t0.Add(3*time.Minute))
	deliveries, _ := h.readers()
	failed := entity.StatusFailed
	ctx := context.Background()
	q := repository.EndpointDeliveriesQuery{ProjectID: projectA, EndpointID: "ep_A", Status: &failed, Limit: 1}

	first, err1 := deliveries.ListByEndpoint(ctx, q)
	q.After = &entity.PageAnchor{CreatedAt: first[0].CreatedAt, ID: first[0].ID}
	second, err2 := deliveries.ListByEndpoint(ctx, q)
	q.After = &entity.PageAnchor{CreatedAt: second[0].CreatedAt, ID: second[0].ID}
	third, err3 := deliveries.ListByEndpoint(ctx, q)

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("errors: %v %v %v", err1, err2, err3)
	}
	if ids(first) != "dlv_f2" || ids(second) != "dlv_f1" || len(third) != 0 {
		t.Fatalf("failed pages = %q | %q | %q", ids(first), ids(second), ids(third))
	}
}

func TestReadsAreProjectScoped(t *testing.T) {
	h := start(t)
	h.seedStatus(t, "dlv_1", "failed", t0)
	deliveries, attempts := h.readers()
	ctx := context.Background()
	if err := h.attempts.Create(ctx, attemptWith("att_1", 1, responded(500, "no"))); err != nil {
		t.Fatal(err)
	}

	byEndpoint, err1 := deliveries.ListByEndpoint(ctx, repository.EndpointDeliveriesQuery{ProjectID: projectB, EndpointID: "ep_A", Limit: 10})
	byMessage, err2 := deliveries.ListByMessage(ctx, projectB, "msg_dlv_1")
	counts, err3 := deliveries.CountByMessages(ctx, projectB, []string{"msg_dlv_1"})
	listed, err4 := attempts.ListByDelivery(ctx, repository.AttemptsQuery{ProjectID: projectB, DeliveryID: "dlv_1", Limit: 10})
	own, err5 := attempts.ListByDelivery(ctx, repository.AttemptsQuery{ProjectID: projectA, DeliveryID: "dlv_1", Limit: 10})

	for _, err := range []error{err1, err2, err3, err4, err5} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(byEndpoint) != 0 || len(byMessage) != 0 || len(counts) != 0 || len(listed) != 0 {
		t.Fatalf("project B saw project A rows: %d %d %d %d", len(byEndpoint), len(byMessage), len(counts), len(listed))
	}
	if len(own) != 1 {
		t.Fatalf("project A sees %d attempts, want 1", len(own))
	}
}

func TestCountByMessagesGroupsByStatusAndOmitsMessagesWithoutDeliveries(t *testing.T) {
	h := start(t)
	h.seedEndpoint(t, projectA, "ep_A2")
	h.exec(t, `INSERT INTO messages (id, project_id, event_type, payload, created_at)
		VALUES ('msg_m1', $1, 'e', '{}', $2), ('msg_m2', $1, 'e', '{}', $2), ('msg_m3', $1, 'e', '{}', $2)`, projectA, t0)
	h.exec(t, `INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, next_attempt_at, created_at, updated_at) VALUES
		('dlv_a', $1, 'msg_m1', 'ep_A',  'pending',   $2, $2, $2),
		('dlv_b', $1, 'msg_m1', 'ep_A2', 'failed',    NULL, $2, $2),
		('dlv_c', $1, 'msg_m2', 'ep_A',  'succeeded', NULL, $2, $2),
		('dlv_d', $1, 'msg_m2', 'ep_A2', 'succeeded', NULL, $2, $2)`, projectA, t0)
	deliveries, _ := h.readers()

	got, err := deliveries.CountByMessages(context.Background(), projectA, []string{"msg_m1", "msg_m2", "msg_m3"})

	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if got["msg_m1"] != (entity.StatusCounts{Pending: 1, Failed: 1}) || got["msg_m2"] != (entity.StatusCounts{Succeeded: 2}) {
		t.Fatalf("counts = %+v", got)
	}
}

func TestListByMessageReturnsEveryDeliveryOrderedByID(t *testing.T) {
	h := start(t)
	h.seedEndpoint(t, projectA, "ep_A2")
	h.exec(t, `INSERT INTO messages (id, project_id, event_type, payload, created_at) VALUES ('msg_m1', $1, 'e', '{}', $2)`, projectA, t0)
	h.exec(t, `INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, next_attempt_at, last_attempt_at, attempt_count, created_at, updated_at) VALUES
		('dlv_z', $1, 'msg_m1', 'ep_A',  'pending', $2, NULL, 0, $2, $2),
		('dlv_y', $1, 'msg_m1', 'ep_A2', 'failed',  NULL, $2, 1, $2, $2)`, projectA, t0)
	deliveries, _ := h.readers()

	got, err := deliveries.ListByMessage(context.Background(), projectA, "msg_m1")

	if err != nil || ids(got) != "dlv_y,dlv_z" {
		t.Fatalf("got %q, err %v", ids(got), err)
	}
	failed := got[0]
	if failed.Status != entity.StatusFailed || failed.AttemptCount != 1 || failed.NextAttemptAt != nil || failed.LastAttemptAt == nil || !failed.LastAttemptAt.Equal(t0) {
		t.Fatalf("failed delivery = %+v", failed)
	}
}

func TestListAttemptsOldestFirstWithKeysetAndMapping(t *testing.T) {
	h := start(t)
	h.seedStatus(t, "dlv_1", "failed", t0)
	ctx := context.Background()
	for i, o := range []entity.Outcome{responded(500, "boom"), entity.FailedOutcome(entity.ErrorTimeout), responded(200, "")} {
		if err := h.attempts.Create(ctx, attemptWith(fmt.Sprintf("att_%d", i+1), i+1, o)); err != nil {
			t.Fatal(err)
		}
	}
	_, attempts := h.readers()
	q := repository.AttemptsQuery{ProjectID: projectA, DeliveryID: "dlv_1", Limit: 10}

	all, err1 := attempts.ListByDelivery(ctx, q)
	q.AfterNumber, q.Limit = 1, 1
	page, err2 := attempts.ListByDelivery(ctx, q)

	if err1 != nil || err2 != nil || len(all) != 3 || len(page) != 1 || page[0].Number != 2 {
		t.Fatalf("all %d page %+v errors %v %v", len(all), page, err1, err2)
	}
	if all[0].Number != 1 || all[1].Number != 2 || all[2].Number != 3 {
		t.Fatalf("order = %d %d %d", all[0].Number, all[1].Number, all[2].Number)
	}
	assertAttemptMapping(t, all)
}

func assertAttemptMapping(t *testing.T, all []entity.Attempt) {
	t.Helper()
	response, timeout, empty := all[0], all[1], all[2]
	if response.StatusCode != 500 || response.Body.Text != "boom" || response.Err != entity.ErrorNone || response.Trigger != entity.TriggerAutomatic ||
		response.Duration != 25*time.Millisecond || !response.StartedAt.Equal(t0) || response.StartedAt.Location() != time.UTC {
		t.Errorf("response attempt = %+v", response)
	}
	if timeout.StatusCode != 0 || timeout.Body != (entity.ResponseBody{}) || timeout.Err != entity.ErrorTimeout {
		t.Errorf("timeout attempt = %+v", timeout)
	}
	if empty.StatusCode != 200 || empty.Body.Text != "" || empty.Err != entity.ErrorNone {
		t.Errorf("empty-body attempt = %+v", empty)
	}
}

func TestAttemptTruncationFlagSurvivesTheRoundTrip(t *testing.T) {
	h := start(t)
	h.seedStatus(t, "dlv_1", "failed", t0)
	ctx := context.Background()
	if err := h.attempts.Create(ctx, attemptWith("att_1", 1, responded(200, strings.Repeat("x", 5000)))); err != nil {
		t.Fatal(err)
	}
	_, attempts := h.readers()

	got, err := attempts.ListByDelivery(ctx, repository.AttemptsQuery{ProjectID: projectA, DeliveryID: "dlv_1", Limit: 1})

	if err != nil || len(got) != 1 || len(got[0].Body.Text) != entity.MaxResponseBodyBytes || !got[0].Body.Truncated {
		t.Fatalf("got %+v, err %v", got, err)
	}
}
