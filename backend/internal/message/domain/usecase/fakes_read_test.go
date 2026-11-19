package usecase_test

import (
	"context"
	"fmt"
	"time"

	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/repository"
)

// List completes fakeMessages as a MessageRepository; the publish tests never list.
func (f *fakeMessages) List(context.Context, repository.ListQuery) ([]entity.MessageHeader, error) {
	return nil, nil
}

// readMessages is the repository of the read use cases.
type readMessages struct {
	*fakeMessages
	headers   []entity.MessageHeader
	listErr   error
	msg       entity.Message
	getErr    error
	listCalls []repository.ListQuery
}

func (f *readMessages) List(_ context.Context, q repository.ListQuery) ([]entity.MessageHeader, error) {
	f.listCalls = append(f.listCalls, q)
	return f.headers, f.listErr
}

func (f *readMessages) Get(context.Context, string, string) (entity.Message, error) {
	return f.msg, f.getErr
}

// fakeDeliveries counts calls so a test can prove there is no N+1.
type fakeDeliveries struct {
	summaries     map[string]entity.DeliverySummary
	summariesErr  error
	deliveries    []entity.MessageDelivery
	deliveriesErr error
	summaryCalls  [][]string
	listCalls     int
}

func (f *fakeDeliveries) Summaries(_ context.Context, _ string, ids []string) (map[string]entity.DeliverySummary, error) {
	f.summaryCalls = append(f.summaryCalls, ids)
	return f.summaries, f.summariesErr
}

func (f *fakeDeliveries) ListForMessage(context.Context, string, string) ([]entity.MessageDelivery, error) {
	f.listCalls++
	return f.deliveries, f.deliveriesErr
}

func headersAt(n int) []entity.MessageHeader {
	out := make([]entity.MessageHeader, n)
	for i := range out {
		out[i] = entity.MessageHeader{ID: fmt.Sprintf("msg_%d", i), EventType: "a", CreatedAt: fixedNow.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}
