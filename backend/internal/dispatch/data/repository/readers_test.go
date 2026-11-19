package repositoryimpl_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/dispatch/data/model"
	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
)

type fakeReadSource struct {
	row     model.DeliveryRow
	rows    []model.DeliveryRow
	counts  []model.StatusCount
	err     error
	listIn  source.EndpointListParams
	countIn []string
}

func (f *fakeReadSource) GetByID(context.Context, string, string) (model.DeliveryRow, error) {
	return f.row, f.err
}

func (f *fakeReadSource) ListByEndpoint(_ context.Context, p source.EndpointListParams) ([]model.DeliveryRow, error) {
	f.listIn = p
	return f.rows, f.err
}

func (f *fakeReadSource) ListByMessage(context.Context, string, string) ([]model.DeliveryRow, error) {
	return f.rows, f.err
}

func (f *fakeReadSource) CountByMessages(_ context.Context, _ string, ids []string) ([]model.StatusCount, error) {
	f.countIn = ids
	return f.counts, f.err
}

func TestDeliveryReaderGetTranslatesNotFoundAndMapsUTC(t *testing.T) {
	local := time.FixedZone("UTC+7", 7*3600)
	next := now
	row := model.DeliveryRow{ID: "dlv_1", Status: "pending", Trigger: "manual", AttemptCount: 2, NextAttemptAt: &next, CreatedAt: now.In(local)}
	tests := []struct {
		name    string
		src     *fakeReadSource
		wantErr error
	}{
		{"found", &fakeReadSource{row: row}, nil},
		{"not found", &fakeReadSource{err: source.ErrNotFound}, domainerrors.ErrDeliveryNotFound},
		{"storage error", &fakeReadSource{err: errBoom}, errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repositoryimpl.NewDeliveryReader(tt.src).Get(context.Background(), "p", "dlv_1")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.Status != entity.StatusPending || got.Trigger != entity.TriggerManual || got.AttemptCount != 2 ||
				got.CreatedAt.Location() != time.UTC || got.NextAttemptAt.Location() != time.UTC || got.LastAttemptAt != nil {
				t.Fatalf("delivery = %+v", got)
			}
		})
	}
}

func TestDeliveryReaderListByEndpointTranslatesTheQuery(t *testing.T) {
	failed := entity.StatusFailed
	anchor := entity.PageAnchor{CreatedAt: now, ID: "dlv_9"}
	tests := []struct {
		name string
		in   repository.EndpointDeliveriesQuery
		want source.EndpointListParams
	}{
		{"first page, any status", repository.EndpointDeliveriesQuery{ProjectID: "p", EndpointID: "e", Limit: 3},
			source.EndpointListParams{ProjectID: "p", EndpointID: "e", Limit: 3}},
		{"status and cursor", repository.EndpointDeliveriesQuery{ProjectID: "p", EndpointID: "e", Limit: 3, Status: &failed, After: &anchor},
			source.EndpointListParams{ProjectID: "p", EndpointID: "e", Limit: 3, HasStatus: true, Status: "failed", HasAfter: true, AfterCreatedAt: now, AfterID: "dlv_9"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeReadSource{rows: []model.DeliveryRow{{ID: "dlv_1"}}}

			got, err := repositoryimpl.NewDeliveryReader(src).ListByEndpoint(context.Background(), tt.in)

			if err != nil || len(got) != 1 || src.listIn != tt.want {
				t.Fatalf("params %+v, want %+v, rows %d, err %v", src.listIn, tt.want, len(got), err)
			}
		})
	}
}

func TestDeliveryReaderListsReturnStorageErrors(t *testing.T) {
	r := repositoryimpl.NewDeliveryReader(&fakeReadSource{err: errBoom})
	ctx := context.Background()

	_, byEndpoint := r.ListByEndpoint(ctx, repository.EndpointDeliveriesQuery{})
	_, byMessage := r.ListByMessage(ctx, "p", "m")
	_, counts := r.CountByMessages(ctx, "p", []string{"m"})

	for name, err := range map[string]error{"endpoint": byEndpoint, "message": byMessage, "counts": counts} {
		if !errors.Is(err, errBoom) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestCountByMessagesFoldsGroupsAndIgnoresUnknownStatuses(t *testing.T) {
	src := &fakeReadSource{counts: []model.StatusCount{
		{MessageID: "m1", Status: "pending", Count: 1}, {MessageID: "m1", Status: "failed", Count: 2},
		{MessageID: "m2", Status: "succeeded", Count: 3}, {MessageID: "m2", Status: "weird", Count: 9},
	}}

	got, err := repositoryimpl.NewDeliveryReader(src).CountByMessages(context.Background(), "p", []string{"m1", "m2"})

	if err != nil || len(got) != 2 || got["m1"] != (entity.StatusCounts{Pending: 1, Failed: 2}) || got["m2"] != (entity.StatusCounts{Succeeded: 3}) {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if len(src.countIn) != 2 {
		t.Fatalf("source asked for %v", src.countIn)
	}
}

func TestAttemptReaderMapsNullsToWhatDidNotHappen(t *testing.T) {
	status, body, errText := int16(502), "bad gateway", "timeout"
	src := &fakeAttemptSource{rows: []model.Attempt{
		{ID: "att_1", Number: 1, Trigger: "automatic", StartedAt: now, DurationMS: 25, ResponseStatus: &status, ResponseBody: &body, ResponseBodyTruncated: true},
		{ID: "att_2", Number: 2, Trigger: "manual", StartedAt: now, DurationMS: 0, Error: &errText},
	}}

	got, err := repositoryimpl.NewAttemptReader(src).ListByDelivery(context.Background(),
		repository.AttemptsQuery{ProjectID: "p", DeliveryID: "d", AfterNumber: 4, Limit: 5})

	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if src.list != (source.AttemptListParams{ProjectID: "p", DeliveryID: "d", AfterNumber: 4, Limit: 5}) {
		t.Fatalf("source params = %+v", src.list)
	}
	response, failed := got[0], got[1]
	if response.StatusCode != 502 || response.Body != (entity.ResponseBody{Text: "bad gateway", Truncated: true}) ||
		response.Err != entity.ErrorNone || response.Duration != 25*time.Millisecond || response.StartedAt.Location() != time.UTC {
		t.Errorf("response attempt = %+v", response)
	}
	if failed.StatusCode != 0 || failed.Body != (entity.ResponseBody{}) || failed.Err != entity.ErrorTimeout || failed.Trigger != entity.TriggerManual {
		t.Errorf("failed attempt = %+v", failed)
	}
}

func TestAttemptReaderReturnsSourceErrors(t *testing.T) {
	_, err := repositoryimpl.NewAttemptReader(&fakeAttemptSource{err: errBoom}).ListByDelivery(context.Background(), repository.AttemptsQuery{})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}
