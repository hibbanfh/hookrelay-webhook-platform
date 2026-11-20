package usecase_test

import (
	"context"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
)

// fakeDeliveryReader records its calls and answers as configured.
type fakeDeliveryReader struct {
	get        entity.Delivery
	getErr     error
	list       []entity.Delivery
	listErr    error
	byMessage  []entity.Delivery
	counts     map[string]entity.StatusCounts
	countErr   error
	listCalls  []repository.EndpointDeliveriesQuery
	countCalls [][]string
	msgCalls   int
	getCalls   int
}

func (f *fakeDeliveryReader) Get(context.Context, string, string) (entity.Delivery, error) {
	f.getCalls++
	return f.get, f.getErr
}

func (f *fakeDeliveryReader) ListByEndpoint(_ context.Context, q repository.EndpointDeliveriesQuery) ([]entity.Delivery, error) {
	f.listCalls = append(f.listCalls, q)
	return f.list, f.listErr
}

func (f *fakeDeliveryReader) ListByMessage(context.Context, string, string) ([]entity.Delivery, error) {
	f.msgCalls++
	return f.byMessage, f.listErr
}

func (f *fakeDeliveryReader) CountByMessages(_ context.Context, _ string, ids []string) (map[string]entity.StatusCounts, error) {
	f.countCalls = append(f.countCalls, ids)
	return f.counts, f.countErr
}

type fakeAttemptReader struct {
	rows  []entity.Attempt
	err   error
	calls []repository.AttemptsQuery
}

func (f *fakeAttemptReader) ListByDelivery(_ context.Context, q repository.AttemptsQuery) ([]entity.Attempt, error) {
	f.calls = append(f.calls, q)
	return f.rows, f.err
}

// fakeEndpointState answers enabled unless disabled is set.
type fakeEndpointState struct {
	disabled    bool
	err         error
	calls       int
	gotProject  string
	gotEndpoint string
}

func (f *fakeEndpointState) GetState(_ context.Context, projectID, endpointID string) (entity.EndpointState, error) {
	f.calls++
	f.gotProject, f.gotEndpoint = projectID, endpointID
	return entity.EndpointState{Enabled: !f.disabled}, f.err
}

// fakeReplayer records the requeued deliveries and answers with stored, or err.
type fakeReplayer struct {
	stored entity.Delivery
	err    error
	got    []entity.Delivery
}

func (f *fakeReplayer) Requeue(_ context.Context, d entity.Delivery) (entity.Delivery, error) {
	f.got = append(f.got, d)
	return f.stored, f.err
}

func deliveriesAt(n int) []entity.Delivery {
	out := make([]entity.Delivery, n)
	for i := range out {
		out[i] = entity.Delivery{ID: "dlv_" + string(rune('a'+i)), CreatedAt: t0.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}

func attemptsNumbered(n int) []entity.Attempt {
	out := make([]entity.Attempt, n)
	for i := range out {
		out[i] = entity.Attempt{ID: "att_" + string(rune('a'+i)), Number: i + 1}
	}
	return out
}
