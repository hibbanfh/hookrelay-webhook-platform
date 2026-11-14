package usecase_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
)

var t0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// steppedClock returns each time in turn, then keeps returning the last one.
type steppedClock struct {
	mu    sync.Mutex
	times []time.Time
	calls int
}

func (c *steppedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := min(c.calls, len(c.times)-1)
	c.calls++
	return c.times[i]
}

func fixedClock(t time.Time) *steppedClock { return &steppedClock{times: []time.Time{t}} }

type sequenceIDs struct{ n int }

func (g *sequenceIDs) New(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

// fakeQueue records ClaimDue and Complete calls.
type fakeQueue struct {
	rows        []entity.Delivery
	claimErr    error
	completeErr error
	claimNow    time.Time
	claimLimit  int
	claimLease  entity.Lease
	claimCalls  int
	completed   []entity.Delivery
	token       string
	order       *callLog
}

func (q *fakeQueue) ClaimDue(_ context.Context, now time.Time, limit int, lease entity.Lease) ([]entity.Delivery, error) {
	q.claimCalls++
	q.claimNow, q.claimLimit, q.claimLease = now, limit, lease
	return q.rows, q.claimErr
}

func (q *fakeQueue) Complete(_ context.Context, d entity.Delivery, token string) error {
	q.order.add("complete")
	q.completed = append(q.completed, d)
	q.token = token
	return q.completeErr
}

type fakeEndpoints struct {
	lookup repository.TargetLookup
	err    error
	calls  int
	gotIDs []string
}

func (e *fakeEndpoints) GetTargets(_ context.Context, ids []string) (repository.TargetLookup, error) {
	e.calls++
	e.gotIDs = ids
	return e.lookup, e.err
}

type fakeMessages struct {
	payloads map[string][]byte
	err      error
	calls    int
	gotIDs   []string
}

func (m *fakeMessages) GetPayloads(_ context.Context, ids []string) (map[string][]byte, error) {
	m.calls++
	m.gotIDs = ids
	return m.payloads, m.err
}

type fakeAttempts struct {
	err   error
	saved []entity.Attempt
	order *callLog
}

func (a *fakeAttempts) Create(_ context.Context, at entity.Attempt) error {
	a.order.add("create")
	a.saved = append(a.saved, at)
	return a.err
}

type fakeSender struct {
	outcome entity.Outcome
	err     error
	calls   int
	got     entity.SignedRequest
}

func (s *fakeSender) Send(_ context.Context, req entity.SignedRequest) (entity.Outcome, error) {
	s.calls++
	s.got = req
	return s.outcome, s.err
}

// fakeTx runs fn inline and records that a transaction was opened.
type fakeTx struct {
	calls    int
	order    *callLog
	deadline time.Time // the deadline of the context handed to the transaction function
}

func (t *fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	t.calls++
	t.order.add("tx")
	t.deadline, _ = ctx.Deadline()
	return fn(ctx)
}

type callLog struct{ calls []string }

func (l *callLog) add(s string) { l.calls = append(l.calls, s) }

type fakePlanner struct {
	at    time.Time
	ok    bool
	calls int
}

func (p *fakePlanner) Next(int, time.Time) (time.Time, bool) {
	p.calls++
	return p.at, p.ok
}
