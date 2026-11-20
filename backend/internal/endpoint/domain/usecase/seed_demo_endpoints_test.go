package usecase_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/usecase"
)

const (
	healthyURL = "https://hr.example/demo/receiver/proj_1"
	flakyURL   = "https://hr.example/demo/receiver/proj_1?fail_first=2"
)

func demoSpecs() []usecase.DemoEndpointSpec {
	return []usecase.DemoEndpointSpec{
		{URL: healthyURL, Description: "Demo receiver (healthy)"},
		{URL: flakyURL, Description: "Demo receiver (flaky: fails twice)"},
	}
}

type seedFixture struct {
	rec       *recorder
	endpoints *fakeEndpoints
	policy    *fakePolicy
	urls      *fakeURLs
	uc        *usecase.SeedDemoEndpoints
}

func newSeedFixture(max int, existing ...string) seedFixture {
	rec := &recorder{}
	endpoints := &fakeEndpoints{rec: rec}
	policy := &fakePolicy{rec: rec}
	urls := &fakeURLs{rec: rec, urls: map[string]struct{}{}}
	for _, u := range existing {
		urls.urls[u] = struct{}{}
	}
	uc := usecase.NewSeedDemoEndpoints(usecase.SeedDemoEndpointsDeps{
		CreateEndpointDeps: usecase.CreateEndpointDeps{
			Endpoints: endpoints, Policy: policy, IDs: &seqIDs{}, Clock: fixedClock{}, Random: fakeRandom{fill: 5}, Tx: fakeTx{rec: rec},
		},
		URLs: urls,
	}, max)
	return seedFixture{rec: rec, endpoints: endpoints, policy: policy, urls: urls, uc: uc}
}

func TestSeedDemoEndpoints(t *testing.T) {
	in := usecase.SeedDemoEndpointsInput{ProjectID: "proj_1", Endpoints: demoSpecs()}
	tests := []struct {
		name         string
		max          int
		existing     []string
		count        int
		in           usecase.SeedDemoEndpointsInput
		policyErr    error
		countErr     error
		urlsErr      error
		createErr    error
		wantErr      error
		wantField    string
		wantCreated  []string
		wantExisting int
		wantCalls    []string
	}{
		{
			name: "empty project creates both", max: testLimit, in: in, wantCreated: []string{healthyURL, flakyURL},
			wantCalls: []string{"policy", "policy", "tx", "count", "urls", "create", "create"},
		},
		{
			name: "both exist creates nothing", max: testLimit, in: in, existing: []string{healthyURL, flakyURL}, count: 2, wantExisting: 2,
			wantCalls: []string{"policy", "policy", "tx", "count", "urls"},
		},
		{
			name: "one exists creates the other", max: testLimit, in: in, existing: []string{healthyURL}, count: 1,
			wantCreated: []string{flakyURL}, wantExisting: 1, wantCalls: []string{"policy", "policy", "tx", "count", "urls", "create"},
		},
		{
			name: "exactly at the limit after seeding", max: testLimit, in: in, count: testLimit - 2, wantCreated: []string{healthyURL, flakyURL},
			wantCalls: []string{"policy", "policy", "tx", "count", "urls", "create", "create"},
		},
		{
			name: "one slot too few creates nothing", max: testLimit, in: in, count: testLimit - 1, wantErr: domainerrors.ErrLimitReached,
			wantCalls: []string{"policy", "policy", "tx", "count", "urls"},
		},
		{
			name: "full project but nothing missing is fine", max: testLimit, in: in, existing: []string{healthyURL, flakyURL}, count: testLimit, wantExisting: 2,
			wantCalls: []string{"policy", "policy", "tx", "count", "urls"},
		},
		{
			name: "same URL twice in the input is created once", max: testLimit,
			in:          usecase.SeedDemoEndpointsInput{ProjectID: "proj_1", Endpoints: []usecase.DemoEndpointSpec{{URL: healthyURL}, {URL: healthyURL}}},
			wantCreated: []string{healthyURL}, wantExisting: 1, wantCalls: []string{"policy", "policy", "tx", "count", "urls", "create"},
		},
		{
			name: "invalid URL never reaches the policy", max: testLimit,
			in:      usecase.SeedDemoEndpointsInput{ProjectID: "proj_1", Endpoints: []usecase.DemoEndpointSpec{{URL: "ftp://x"}}},
			wantErr: domainerrors.ErrInvalid, wantField: "url",
		},
		{
			name: "invalid description", max: testLimit,
			in:      usecase.SeedDemoEndpointsInput{ProjectID: "proj_1", Endpoints: []usecase.DemoEndpointSpec{{URL: healthyURL, Description: string(make([]byte, 300))}}},
			wantErr: domainerrors.ErrInvalid, wantField: "description",
		},
		{
			name: "policy rejects before the transaction", max: testLimit, in: in, policyErr: errors.New("host must be a public address"),
			wantErr: domainerrors.ErrInvalid, wantField: "url", wantCalls: []string{"policy"},
		},
		{name: "count fails", max: testLimit, in: in, countErr: errBoom, wantErr: errBoom, wantCalls: []string{"policy", "policy", "tx", "count"}},
		{name: "listing URLs fails", max: testLimit, in: in, urlsErr: errBoom, wantErr: errBoom, wantCalls: []string{"policy", "policy", "tx", "count", "urls"}},
		{name: "insert fails", max: testLimit, in: in, createErr: errBoom, wantErr: errBoom, wantCalls: []string{"policy", "policy", "tx", "count", "urls", "create"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSeedFixture(tt.max, tt.existing...)
			f.endpoints.count, f.endpoints.countErr, f.endpoints.createErr = tt.count, tt.countErr, tt.createErr
			f.policy.err, f.urls.err = tt.policyErr, tt.urlsErr

			out, err := f.uc.Execute(context.Background(), tt.in)

			assertError(t, err, tt.wantErr, tt.wantField)
			assertCalls(t, f.rec.calls, tt.wantCalls)
			assertSeeded(t, f, out, tt.wantCreated, tt.wantExisting)
		})
	}
}

// assertSeeded compares what was stored and what the output reports with the expectation. A
// failed run reports nothing, so its expectation is the zero value.
func assertSeeded(t *testing.T, f seedFixture, out usecase.SeedDemoEndpointsOutput, wantCreated []string, wantExisting int) {
	t.Helper()
	stored := urlsOf(f.endpoints.created)
	reported := urlsOf(out.Created)
	if !slices.Equal(stored, wantCreated) || !slices.Equal(reported, stored) || out.Existing != wantExisting {
		t.Fatalf("stored %v, reported %v, existing %d; want %v and %d", stored, reported, out.Existing, wantCreated, wantExisting)
	}
}

func urlsOf(endpoints []entity.Endpoint) []string {
	out := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		out = append(out, e.URL)
	}
	return out
}

func TestSeedDemoEndpointsCreatesEnabledEndpointsWithDistinctIDsAndSecrets(t *testing.T) {
	f := newSeedFixture(testLimit)

	out, err := f.uc.Execute(context.Background(), usecase.SeedDemoEndpointsInput{ProjectID: "proj_1", Endpoints: demoSpecs()})

	if err != nil || len(out.Created) != 2 || len(f.endpoints.secrets) != 2 {
		t.Fatalf("out %+v, err %v, %d secrets", out, err, len(f.endpoints.secrets))
	}
	first, second := out.Created[0], out.Created[1]
	if first.ID == second.ID || !first.Enabled || !second.Enabled || first.ProjectID != "proj_1" ||
		first.Description != "Demo receiver (healthy)" || !first.CreatedAt.Equal(fixedNow) || len(f.endpoints.secrets[0].Bytes()) != 32 {
		t.Fatalf("created %+v and %+v", first, second)
	}
}
