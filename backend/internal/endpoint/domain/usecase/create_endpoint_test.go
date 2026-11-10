package usecase_test

import (
	"errors"
	"testing"

	"context"

	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/usecase"
)

const testLimit = 20

type createFixture struct {
	rec       *recorder
	endpoints *fakeEndpoints
	policy    *fakePolicy
	uc        *usecase.CreateEndpoint
}

func newCreateFixture(random fakeRandom, tx error) createFixture {
	rec := &recorder{}
	endpoints := &fakeEndpoints{rec: rec}
	policy := &fakePolicy{rec: rec}
	uc := usecase.NewCreateEndpoint(usecase.CreateEndpointDeps{
		Endpoints: endpoints, Policy: policy, IDs: &seqIDs{}, Clock: fixedClock{},
		Random: random, Tx: fakeTx{rec: rec, err: tx},
	}, testLimit)
	return createFixture{rec: rec, endpoints: endpoints, policy: policy, uc: uc}
}

func TestCreateEndpoint(t *testing.T) {
	valid := usecase.CreateEndpointInput{ProjectID: "proj_1", URL: "https://example.com/hook", Description: "Acme prod"}
	tests := []struct {
		name        string
		in          usecase.CreateEndpointInput
		count       int
		countErr    error
		createErr   error
		policyErr   error
		randomErr   error
		txErr       error
		wantErr     error
		wantField   string
		wantCalls   []string
		wantCreated int
	}{
		{name: "valid", in: valid, wantCalls: []string{"policy", "tx", "count", "create"}, wantCreated: 1},
		{name: "one below the limit", in: valid, count: testLimit - 1, wantCalls: []string{"policy", "tx", "count", "create"}, wantCreated: 1},
		{name: "at the limit", in: valid, count: testLimit, wantErr: domainerrors.ErrLimitReached, wantCalls: []string{"policy", "tx", "count"}},
		{name: "above the limit", in: valid, count: testLimit + 5, wantErr: domainerrors.ErrLimitReached, wantCalls: []string{"policy", "tx", "count"}},
		{name: "invalid url never reaches the policy", in: usecase.CreateEndpointInput{ProjectID: "proj_1", URL: "ftp://x"}, wantErr: domainerrors.ErrInvalid, wantField: "url"},
		{name: "invalid description never reaches the policy", in: usecase.CreateEndpointInput{ProjectID: "proj_1", URL: "https://example.com", Description: string(make([]byte, 300))}, wantErr: domainerrors.ErrInvalid, wantField: "description"},
		{name: "policy rejects the destination", in: valid, policyErr: errors.New("host must be a public address"), wantErr: domainerrors.ErrInvalid, wantField: "url", wantCalls: []string{"policy"}},
		{name: "randomness fails", in: valid, randomErr: errBoom, wantErr: errBoom, wantCalls: []string{"policy"}},
		{name: "count fails", in: valid, countErr: errBoom, wantErr: errBoom, wantCalls: []string{"policy", "tx", "count"}},
		{name: "insert fails", in: valid, createErr: errBoom, wantErr: errBoom, wantCalls: []string{"policy", "tx", "count", "create"}},
		{name: "transaction cannot begin", in: valid, txErr: errBoom, wantErr: errBoom, wantCalls: []string{"policy", "tx"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCreateFixture(fakeRandom{fill: 5, err: tt.randomErr}, tt.txErr)
			f.endpoints.count, f.endpoints.countErr, f.endpoints.createErr, f.policy.err = tt.count, tt.countErr, tt.createErr, tt.policyErr

			_, err := f.uc.Execute(context.Background(), tt.in)

			assertError(t, err, tt.wantErr, tt.wantField)
			assertCalls(t, f.rec.calls, tt.wantCalls)
			if len(f.endpoints.created) != tt.wantCreated {
				t.Fatalf("created %d endpoints, want %d", len(f.endpoints.created), tt.wantCreated)
			}
		})
	}
}

func TestCreateEndpointLimitErrorNamesTheLimit(t *testing.T) {
	f := newCreateFixture(fakeRandom{fill: 5}, nil)
	f.endpoints.count = testLimit

	_, err := f.uc.Execute(context.Background(), usecase.CreateEndpointInput{ProjectID: "proj_1", URL: "https://example.com/hook"})

	var limit *domainerrors.LimitReachedError
	if !errors.As(err, &limit) || limit.Limit != testLimit {
		t.Fatalf("got %v", err)
	}
}

func TestCreateEndpointReturnsTheStoredEndpointAndSecret(t *testing.T) {
	f := newCreateFixture(fakeRandom{fill: 5}, nil)
	in := usecase.CreateEndpointInput{ProjectID: "proj_1", URL: "https://example.com/hook", Description: "Acme prod"}

	out, err := f.uc.Execute(context.Background(), in)

	stored := f.endpoints.created
	if err != nil || len(stored) != 1 || stored[0] != out.Endpoint || out.Endpoint.ID != "ep_1" || !out.Endpoint.Enabled ||
		out.Endpoint.ProjectID != "proj_1" || !out.Endpoint.CreatedAt.Equal(fixedNow) ||
		len(out.Secret.Bytes()) != 32 || out.Secret.Value() != f.endpoints.secrets[0].Value() {
		t.Fatalf("out %+v, stored %+v, err %v", out, stored, err)
	}
}

func TestCreateEndpointSecretsDifferWhenTheRandomnessDiffers(t *testing.T) {
	a := newCreateFixture(fakeRandom{fill: 1}, nil)
	b := newCreateFixture(fakeRandom{fill: 2}, nil)
	in := usecase.CreateEndpointInput{ProjectID: "proj_1", URL: "https://example.com/hook"}

	outA, errA := a.uc.Execute(context.Background(), in)
	outB, errB := b.uc.Execute(context.Background(), in)

	if errA != nil || errB != nil || outA.Secret.Value() == outB.Secret.Value() {
		t.Fatalf("secrets must differ: %v %v", errA, errB)
	}
}
