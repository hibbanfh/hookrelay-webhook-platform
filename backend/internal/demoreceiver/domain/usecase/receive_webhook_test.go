package usecase_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"hookrelay/internal/demoreceiver/domain/entity"
	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
	"hookrelay/internal/demoreceiver/domain/repository"
	"hookrelay/internal/demoreceiver/domain/usecase"
)

const (
	testProject = "proj_01JC3ZK6Q8V2W7T4N5R9XBYH0M"
	validSig    = "v1,valid"
)

type receiveFixture struct {
	secrets *fakeSecrets
	hits    *fakeHits
	uc      *usecase.ReceiveWebhook
}

func newReceiveFixture(cands []entity.Candidate, counter int) receiveFixture {
	secrets := &fakeSecrets{candidates: cands}
	hits := &fakeHits{n: counter}
	signer := fakeSigner{byKey: map[string]string{"key-one": "v1,one", "key-two": validSig, "key-three": "v1,three"}}
	return receiveFixture{secrets: secrets, hits: hits, uc: usecase.NewReceiveWebhook(secrets, signer, hits, fixedClock{})}
}

func inputAt(offsetSeconds int64, signature string, failFirst int) usecase.ReceiveWebhookInput {
	return usecase.ReceiveWebhookInput{
		ProjectID: testProject,
		Plan:      planOf(failFirst),
		Headers: entity.WebhookHeaders{
			ID: "msg_1", Timestamp: strconv.FormatInt(fixedNow.Unix()+offsetSeconds, 10), Signature: signature,
		},
		Body: []byte(`{"a":1}`),
	}
}

func twoCandidates() []entity.Candidate {
	return []entity.Candidate{candidate("ep_first", "key-one"), candidate("ep_second", "key-two")}
}

func TestReceiveWebhookRefusesBeforeAnyLookup(t *testing.T) {
	malformed := inputAt(0, validSig, 0)
	malformed.Headers.ID = ""
	tests := []struct {
		name    string
		in      usecase.ReceiveWebhookInput
		wantErr error
	}{
		{name: "malformed headers", in: malformed, wantErr: domainerrors.ErrInvalidSignature},
		{name: "timestamp 301 seconds old", in: inputAt(-301, validSig, 0), wantErr: domainerrors.ErrStaleTimestamp},
		{name: "timestamp 301 seconds ahead", in: inputAt(301, validSig, 0), wantErr: domainerrors.ErrStaleTimestamp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReceiveFixture(twoCandidates(), 1)

			_, err := f.uc.Execute(context.Background(), tt.in)

			if !errors.Is(err, tt.wantErr) || f.secrets.calls != 0 || len(f.hits.got) != 0 {
				t.Fatalf("error %v (want %v), %d secret lookups, %d increments; want none", err, tt.wantErr, f.secrets.calls, len(f.hits.got))
			}
		})
	}
}

func TestReceiveWebhookStaleTimestampIsUnauthorized(t *testing.T) {
	f := newReceiveFixture(twoCandidates(), 1)

	_, err := f.uc.Execute(context.Background(), inputAt(-301, validSig, 0))

	if !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("got %v, want a wrapped ErrUnauthorized", err)
	}
}

func TestReceiveWebhookRefusesWithoutACountedHit(t *testing.T) {
	tests := []struct {
		name       string
		candidates []entity.Candidate
		signature  string
	}{
		{name: "unknown project", candidates: []entity.Candidate{}, signature: validSig},
		{name: "project without candidates", candidates: nil, signature: validSig},
		{name: "wrong signature", candidates: twoCandidates(), signature: "v1,forged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReceiveFixture(tt.candidates, 1)

			_, err := f.uc.Execute(context.Background(), inputAt(0, tt.signature, 0))

			if !errors.Is(err, domainerrors.ErrInvalidSignature) || f.secrets.calls != 1 || len(f.hits.got) != 0 {
				t.Fatalf("error %v, %d lookups, %d increments; want ErrInvalidSignature, 1, 0", err, f.secrets.calls, len(f.hits.got))
			}
		})
	}
}

func TestReceiveWebhookAcceptsTheEdgeOfTheTolerance(t *testing.T) {
	f := newReceiveFixture(twoCandidates(), 1)

	_, err := f.uc.Execute(context.Background(), inputAt(-300, validSig, 0))

	if err != nil {
		t.Fatalf("a timestamp exactly 300 s old must be accepted: %v", err)
	}
}

func TestReceiveWebhookCountsForTheEndpointWhoseSecretVerified(t *testing.T) {
	f := newReceiveFixture(twoCandidates(), 1)

	_, err := f.uc.Execute(context.Background(), inputAt(0, "v1,other "+validSig, 0))

	want := repository.HitIncrement{
		Key: entity.HitKey{EndpointID: "ep_second", WebhookID: "msg_1"}, At: fixedNow, ExpiredBefore: fixedNow.Add(-entity.HitRetention),
	}
	if err != nil || len(f.hits.got) != 1 || f.hits.got[0] != want || f.secrets.gotProject != testProject {
		t.Fatalf("err %v, increments %+v (want %+v), project %q", err, f.hits.got, want, f.secrets.gotProject)
	}
}

func TestReceiveWebhookFailsWithinTheBudget(t *testing.T) {
	tests := []struct {
		name    string
		counter int
		want    domainerrors.SimulatedFailure
	}{
		{name: "first request", counter: 1, want: domainerrors.SimulatedFailure{RequestNumber: 1, FailFirst: 2}},
		{name: "second request", counter: 2, want: domainerrors.SimulatedFailure{RequestNumber: 2, FailFirst: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReceiveFixture(twoCandidates(), tt.counter)

			_, err := f.uc.Execute(context.Background(), inputAt(0, validSig, 2))

			var failure *domainerrors.SimulatedFailure
			if !errors.As(err, &failure) || *failure != tt.want {
				t.Fatalf("got %v, want %+v", err, tt.want)
			}
		})
	}
}

func TestReceiveWebhookSucceedsAfterTheBudget(t *testing.T) {
	tests := []struct {
		name      string
		counter   int
		failFirst int
		want      entity.Receipt
	}{
		{name: "third request of two", counter: 3, failFirst: 2, want: entity.Receipt{EndpointID: "ep_second", WebhookID: "msg_1", RequestNumber: 3, FailFirst: 2}},
		{name: "no budget", counter: 1, failFirst: 0, want: entity.Receipt{EndpointID: "ep_second", WebhookID: "msg_1", RequestNumber: 1, FailFirst: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReceiveFixture(twoCandidates(), tt.counter)

			got, err := f.uc.Execute(context.Background(), inputAt(0, validSig, tt.failFirst))

			if err != nil || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestReceiveWebhookWrapsStorageFailures(t *testing.T) {
	tests := []struct {
		name       string
		secretsErr error
		hitsErr    error
	}{
		{name: "secret lookup fails", secretsErr: errBoom},
		{name: "counter fails", hitsErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReceiveFixture(twoCandidates(), 1)
			f.secrets.err, f.hits.err = tt.secretsErr, tt.hitsErr

			_, err := f.uc.Execute(context.Background(), inputAt(0, validSig, 2))

			var failure *domainerrors.SimulatedFailure
			if !errors.Is(err, errBoom) || errors.Is(err, domainerrors.ErrUnauthorized) || errors.As(err, &failure) {
				t.Fatalf("got %v, want a wrapped errBoom that is neither 401 nor a simulated failure", err)
			}
		})
	}
}
