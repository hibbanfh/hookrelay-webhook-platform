package usecase_test

import (
	"context"
	"errors"
	"time"

	"hookrelay/internal/demoreceiver/domain/entity"
	"hookrelay/internal/demoreceiver/domain/repository"
)

var (
	fixedNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	errBoom  = errors.New("boom")
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

// fakeSecrets returns a fixed candidate list and counts the calls.
type fakeSecrets struct {
	candidates []entity.Candidate
	err        error
	calls      int
	gotProject string
}

func (f *fakeSecrets) DemoSecrets(_ context.Context, projectID string) ([]entity.Candidate, error) {
	f.calls++
	f.gotProject = projectID
	return f.candidates, f.err
}

// fakeSigner signs with a fixed string per key, so the tests stay inside the module: the key
// bytes are the lookup key of the signature the "HMAC" would produce.
type fakeSigner struct{ byKey map[string]string }

func (f fakeSigner) Sign(key []byte, _ string, _ int64, _ []byte) string { return f.byKey[string(key)] }

// fakeHits answers the scripted counter and records every increment.
type fakeHits struct {
	n   int
	err error
	got []repository.HitIncrement
}

func (f *fakeHits) Increment(_ context.Context, in repository.HitIncrement) (int, error) {
	f.got = append(f.got, in)
	return f.n, f.err
}

// planOf builds a plan the only way production code can. The inputs of these tests are in range.
func planOf(failFirst int) entity.FailurePlan {
	plan, err := entity.NewFailurePlan(failFirst)
	if err != nil {
		panic(err) // a test table with an out-of-range budget is a bug in the test
	}
	return plan
}

func candidate(endpointID, key string) entity.Candidate {
	return entity.Candidate{EndpointID: endpointID, Key: entity.NewSigningKey([]byte(key))}
}
