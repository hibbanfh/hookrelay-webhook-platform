package wire

import (
	"context"
	"errors"
	"testing"
	"time"

	dispatchentity "hookrelay/internal/dispatch/domain/entity"
	dispatcherrors "hookrelay/internal/dispatch/domain/errors"
	dispatchrepo "hookrelay/internal/dispatch/domain/repository"
	dispatchuc "hookrelay/internal/dispatch/domain/usecase"
	endpointentity "hookrelay/internal/endpoint/domain/entity"
	endpointerrors "hookrelay/internal/endpoint/domain/errors"
	endpointrepository "hookrelay/internal/endpoint/domain/repository"
	endpointuc "hookrelay/internal/endpoint/domain/usecase"
	messageentity "hookrelay/internal/message/domain/entity"
	messagerepository "hookrelay/internal/message/domain/repository"
)

var readAt = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

// fakeDispatchDeliveries is the dispatch DeliveryReader the real use cases run on.
type fakeDispatchDeliveries struct {
	counts       map[string]dispatchentity.StatusCounts
	rows         []dispatchentity.Delivery
	err          error
	countCalls   int
	listCalls    int
	askedIDs     []string
	askedProject string
}

func (f *fakeDispatchDeliveries) Get(context.Context, string, string) (dispatchentity.Delivery, error) {
	return dispatchentity.Delivery{}, nil
}

func (f *fakeDispatchDeliveries) ListByEndpoint(context.Context, dispatchrepo.EndpointDeliveriesQuery) ([]dispatchentity.Delivery, error) {
	return nil, nil
}

func (f *fakeDispatchDeliveries) ListByMessage(_ context.Context, projectID, _ string) ([]dispatchentity.Delivery, error) {
	f.listCalls++
	f.askedProject = projectID
	return f.rows, f.err
}

func (f *fakeDispatchDeliveries) CountByMessages(_ context.Context, projectID string, ids []string) (map[string]dispatchentity.StatusCounts, error) {
	f.countCalls++
	f.askedProject, f.askedIDs = projectID, ids
	return f.counts, f.err
}

func newDeliveryReaderOn(f *fakeDispatchDeliveries) messagerepository.DeliveryReader {
	return ProvideDeliveryReader(dispatchuc.NewSummarizeDeliveries(f), dispatchuc.NewListMessageDeliveries(f))
}

func TestDeliveryReaderSummariesMapsCountsAndFillsZeros(t *testing.T) {
	f := &fakeDispatchDeliveries{counts: map[string]dispatchentity.StatusCounts{"msg_1": {Pending: 1, Succeeded: 2, Failed: 3}}}

	got, err := newDeliveryReaderOn(f).Summaries(context.Background(), "proj_1", []string{"msg_1", "msg_2"})

	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if got["msg_1"] != (messageentity.DeliverySummary{Pending: 1, Succeeded: 2, Failed: 3}) || got["msg_2"] != (messageentity.DeliverySummary{}) {
		t.Fatalf("summaries = %+v", got)
	}
	if f.askedProject != "proj_1" || len(f.askedIDs) != 2 || f.countCalls != 1 {
		t.Fatalf("project %q ids %v calls %d", f.askedProject, f.askedIDs, f.countCalls)
	}
}

func TestDeliveryReaderSummariesOfNothingMakesNoQuery(t *testing.T) {
	f := &fakeDispatchDeliveries{}

	got, err := newDeliveryReaderOn(f).Summaries(context.Background(), "proj_1", nil)

	if err != nil || len(got) != 0 || f.countCalls != 0 {
		t.Fatalf("got %+v, calls %d, err %v", got, f.countCalls, err)
	}
}

func TestDeliveryReaderListForMessageMapsEveryField(t *testing.T) {
	next, last := readAt.Add(time.Minute), readAt.Add(-time.Minute)
	f := &fakeDispatchDeliveries{rows: []dispatchentity.Delivery{
		{
			ID: "dlv_1", ProjectID: "proj_1", MessageID: "msg_1", EndpointID: "ep_1", Status: dispatchentity.StatusPending,
			AttemptCount: 2, NextAttemptAt: &next, LastAttemptAt: &last, CreatedAt: readAt,
		},
		{ID: "dlv_2", MessageID: "msg_1", EndpointID: "ep_2", Status: dispatchentity.StatusFailed, CreatedAt: readAt},
	}}

	got, err := newDeliveryReaderOn(f).ListForMessage(context.Background(), "proj_1", "msg_1")

	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	first, second := got[0], got[1]
	if first.ID != "dlv_1" || first.MessageID != "msg_1" || first.EndpointID != "ep_1" || first.Status != "pending" || first.AttemptCount != 2 ||
		first.NextAttemptAt == nil || !first.NextAttemptAt.Equal(next) || first.LastAttemptAt == nil || !first.LastAttemptAt.Equal(last) || !first.CreatedAt.Equal(readAt) {
		t.Fatalf("first = %+v", first)
	}
	if second.Status != "failed" || second.NextAttemptAt != nil || second.LastAttemptAt != nil {
		t.Fatalf("second = %+v", second)
	}
}

func TestDeliveryReaderListForMessageWithNoDeliveriesIsEmptyNotNil(t *testing.T) {
	got, err := newDeliveryReaderOn(&fakeDispatchDeliveries{}).ListForMessage(context.Background(), "proj_1", "msg_1")

	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %v (nil=%v), err %v", got, got == nil, err)
	}
}

func TestDeliveryReaderWrapsDispatchErrors(t *testing.T) {
	r := newDeliveryReaderOn(&fakeDispatchDeliveries{err: errAdapterBoom})

	_, summaries := r.Summaries(context.Background(), "p", []string{"msg_1"})
	_, list := r.ListForMessage(context.Background(), "p", "msg_1")

	if !errors.Is(summaries, errAdapterBoom) || !errors.Is(list, errAdapterBoom) {
		t.Fatalf("got %v / %v", summaries, list)
	}
}

// endpointGetter fakes the endpoint repository behind the real GetEndpoint use case.
type endpointGetter struct {
	endpointrepository.EndpointRepository
	endpoint endpointentity.Endpoint
	err      error
	gotProj  string
	gotID    string
}

func (f *endpointGetter) Get(_ context.Context, projectID, id string) (endpointentity.Endpoint, error) {
	f.gotProj, f.gotID = projectID, id
	return f.endpoint, f.err
}

func TestEndpointStateReaderMapsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{{"enabled", true}, {"disabled", false}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &endpointGetter{endpoint: endpointentity.Endpoint{ID: "ep_1", Enabled: tt.enabled}}

			got, err := ProvideEndpointStateReader(endpointuc.NewGetEndpoint(repo)).GetState(context.Background(), "proj_1", "ep_1")

			if err != nil || got.Enabled != tt.enabled || repo.gotProj != "proj_1" || repo.gotID != "ep_1" {
				t.Fatalf("got %+v, err %v, asked %q/%q", got, err, repo.gotProj, repo.gotID)
			}
		})
	}
}

func TestEndpointStateReaderTranslatesNotFoundToTheDispatchError(t *testing.T) {
	repo := &endpointGetter{err: endpointerrors.ErrNotFound}

	_, err := ProvideEndpointStateReader(endpointuc.NewGetEndpoint(repo)).GetState(context.Background(), "proj_1", "ep_1")

	if !errors.Is(err, dispatcherrors.ErrEndpointNotFound) || !errors.Is(err, dispatcherrors.ErrNotFound) || errors.Is(err, endpointerrors.ErrNotFound) {
		t.Fatalf("got %v, want the dispatch error only", err)
	}
}

func TestEndpointStateReaderWrapsOtherErrors(t *testing.T) {
	repo := &endpointGetter{err: errAdapterBoom}

	_, err := ProvideEndpointStateReader(endpointuc.NewGetEndpoint(repo)).GetState(context.Background(), "proj_1", "ep_1")

	if !errors.Is(err, errAdapterBoom) || errors.Is(err, dispatcherrors.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
