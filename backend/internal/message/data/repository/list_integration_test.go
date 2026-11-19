//go:build integration

package repositoryimpl_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/repository"
)

// seedHeaders inserts messages of a project at explicit times, with payloads that would show
// up in a payload leak.
func (h harness) seedHeaders(t *testing.T, projectID string, idsAndTimes map[string]time.Time) {
	t.Helper()
	for id, at := range idsAndTimes {
		h.exec(t, `INSERT INTO messages (id, project_id, event_type, payload, created_at) VALUES ($1, $2, 'invoice.paid', '{"secret":"x"}', $3)`, id, projectID, at)
	}
}

func headerIDs(hs []entity.MessageHeader) string {
	ids := make([]string, 0, len(hs))
	for _, h := range hs {
		ids = append(ids, h.ID)
	}
	return strings.Join(ids, ",")
}

func TestListIsNewestFirstAndPagesWithATieOnCreatedAt(t *testing.T) {
	h := start(t)
	tie := t0.Add(time.Hour)
	h.seedHeaders(t, projectA, map[string]time.Time{
		"msg_old": t0, "msg_a": tie, "msg_b": tie, "msg_c": tie,
	})
	ctx := context.Background()
	q := repository.ListQuery{ProjectID: projectA, Limit: 2}

	first, err1 := h.messages.List(ctx, q)
	q.After = &entity.PageAnchor{CreatedAt: first[1].CreatedAt, ID: first[1].ID}
	second, err2 := h.messages.List(ctx, q)

	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v %v", err1, err2)
	}
	if headerIDs(first) != "msg_c,msg_b" || headerIDs(second) != "msg_a,msg_old" {
		t.Fatalf("pages = %q | %q", headerIDs(first), headerIDs(second))
	}
	if first[0].EventType != "invoice.paid" || first[0].ProjectID != projectA || !first[0].CreatedAt.Equal(tie) || first[0].CreatedAt.Location() != time.UTC {
		t.Fatalf("header = %+v", first[0])
	}
}

func TestListIsProjectScoped(t *testing.T) {
	h := start(t)
	h.seedHeaders(t, projectA, map[string]time.Time{"msg_a": t0})
	h.seedHeaders(t, projectB, map[string]time.Time{"msg_b": t0})

	got, err := h.messages.List(context.Background(), repository.ListQuery{ProjectID: projectB, Limit: 10})

	if err != nil || headerIDs(got) != "msg_b" {
		t.Fatalf("got %q, err %v", headerIDs(got), err)
	}
}
