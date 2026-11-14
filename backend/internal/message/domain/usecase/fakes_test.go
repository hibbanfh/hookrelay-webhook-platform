package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/usecase"
)

const keyTTL = 24 * time.Hour

var errBoom = errors.New("boom")

// fixedNow has a sub-microsecond part and a non-UTC zone, so the tests prove the use case
// normalizes both.
var fixedNow = time.Date(2026, 11, 13, 16, 30, 0, 123456789, time.FixedZone("UTC+7", 7*3600))

type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

type seqIDs struct{ n int }

func (s *seqIDs) New(prefix string) string {
	s.n++
	return fmt.Sprintf("%s%d", prefix, s.n)
}

// recorder remembers the order of calls across fakes.
type recorder struct{ calls []string }

func (r *recorder) add(call string) { r.calls = append(r.calls, call) }

// fakeTx runs fn like a real TxManager and returns its error.
type fakeTx struct{ rec *recorder }

func (f fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.rec.add("tx")
	return fn(ctx)
}

type fakeMessages struct {
	rec       *recorder
	created   []entity.Message
	stored    map[string]entity.Message
	createErr error
	getErr    error
}

func (f *fakeMessages) Create(_ context.Context, m entity.Message) error {
	f.rec.add("create")
	f.created = append(f.created, m)
	return f.createErr
}

func (f *fakeMessages) Get(_ context.Context, _, id string) (entity.Message, error) {
	f.rec.add("get")
	return f.stored[id], f.getErr
}

type fakeKeys struct {
	rec        *recorder
	won        bool
	reserveErr error
	existing   entity.IdempotencyRecord
	findErr    error
	reserved   []entity.IdempotencyRecord
}

func (f *fakeKeys) Reserve(_ context.Context, r entity.IdempotencyRecord) (bool, error) {
	f.rec.add("reserve")
	f.reserved = append(f.reserved, r)
	return f.won, f.reserveErr
}

func (f *fakeKeys) Find(context.Context, string, entity.IdempotencyKey) (entity.IdempotencyRecord, error) {
	f.rec.add("find")
	return f.existing, f.findErr
}

type fakeDirectory struct {
	rec *recorder
	ids []string
	err error
}

func (f *fakeDirectory) EnabledEndpointIDs(context.Context, string) ([]string, error) {
	f.rec.add("endpoints")
	return f.ids, f.err
}

type fakeOutbox struct {
	rec      *recorder
	enqueued [][]entity.PendingDelivery
	err      error
}

func (f *fakeOutbox) Enqueue(_ context.Context, d []entity.PendingDelivery) error {
	f.rec.add("enqueue")
	f.enqueued = append(f.enqueued, d)
	return f.err
}

// fixture wires the use case to fakes. Keys win the reservation by default.
type fixture struct {
	rec    *recorder
	msgs   *fakeMessages
	keys   *fakeKeys
	dir    *fakeDirectory
	outbox *fakeOutbox
	uc     *usecase.PublishMessage
}

func newFixture() *fixture {
	rec := &recorder{}
	f := &fixture{
		rec:    rec,
		msgs:   &fakeMessages{rec: rec, stored: map[string]entity.Message{}},
		keys:   &fakeKeys{rec: rec, won: true},
		dir:    &fakeDirectory{rec: rec},
		outbox: &fakeOutbox{rec: rec},
	}
	f.uc = usecase.NewPublishMessage(usecase.PublishMessageDeps{
		Messages: f.msgs, Keys: f.keys, Endpoints: f.dir, Outbox: f.outbox,
		IDs: &seqIDs{}, Clock: fixedClock{}, Tx: fakeTx{rec: rec},
	}, keyTTL)
	return f
}
