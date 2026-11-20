//go:build integration

package repositoryimpl_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	repositoryimpl "hookrelay/internal/endpoint/data/repository"
	"hookrelay/internal/endpoint/data/source"
	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/random"
)

const demoBase = "https://hr.example/demo/receiver"

// createWithURL stores an endpoint with an arbitrary URL and a recognisable secret.
func (h harness) createWithURL(t *testing.T, projectID, id, url string, createdAt time.Time) entity.SigningSecret {
	t.Helper()
	e, err := entity.NewEndpoint(entity.EndpointSpec{ID: id, ProjectID: projectID, URL: url, Description: "d"}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := entity.NewSigningSecret(bytes.Repeat([]byte{byte(len(id))}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.Create(context.Background(), e, secret); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	return secret
}

func repositoryURLLister(h harness) repository.EndpointURLLister {
	return repositoryimpl.NewEndpointURLLister(source.NewEndpointPersistent(h.pool))
}

func endpointIDsOf(batch repository.DemoSecretBatch) []string {
	out := make([]string, 0, len(batch.Secrets))
	for _, r := range batch.Secrets {
		out = append(out, r.EndpointID)
	}
	return out
}

func TestListDemoSecretsMatchesOnlyTheExactDemoURLsOfTheProject(t *testing.T) {
	h := start(t)
	receiver := demoBase + "/" + projectA
	h.createWithURL(t, projectA, epID(1), receiver, base)
	h.createWithURL(t, projectA, epID(2), receiver+"?fail_first=2", base.Add(time.Second))
	// None of the rows below may be a candidate.
	h.createWithURL(t, projectA, epID(3), receiver+"/x", base.Add(2*time.Second))
	h.createWithURL(t, projectA, epID(4), receiver+"extra", base.Add(3*time.Second))
	h.createWithURL(t, projectA, epID(5), demoBase+"/"+projectA+"2", base.Add(4*time.Second))
	h.createWithURL(t, projectA, epID(6), "https://other.example/demo/receiver/"+projectA, base.Add(5*time.Second))
	h.createWithURL(t, projectA, epID(7), "http://hr.example/demo/receiver/"+projectA, base.Add(6*time.Second))
	h.createWithURL(t, projectB, epID(8), receiver, base.Add(7*time.Second))
	// "_" is a LIKE wildcard; the match must be literal.
	h.createWithURL(t, projectA, epID(9), demoBase+"/projXA", base.Add(8*time.Second))
	h.createWithURL(t, projectA, epID(10), demoBase+"/projXA?fail_first=2", base.Add(9*time.Second))

	got, err := h.secrets.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: projectA, ReceiverURL: receiver, Limit: 20})

	if err != nil || !slices.Equal(endpointIDsOf(got), []string{epID(1), epID(2)}) {
		t.Fatalf("candidates %v, err %v; want only the exact URL and the URL with a query", endpointIDsOf(got), err)
	}
}

func TestListDemoSecretsReturnsTheSecretOfEachEndpoint(t *testing.T) {
	h := start(t)
	receiver := demoBase + "/" + projectA
	first := h.createWithURL(t, projectA, epID(1), receiver, base)
	second := h.createWithURL(t, projectA, epID(22), receiver+"?fail_first=2", base.Add(time.Second))

	got, err := h.secrets.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: projectA, ReceiverURL: receiver, Limit: 20})

	if err != nil || len(got.Secrets) != 2 || !bytes.Equal(got.Secrets[0].Secret.Bytes(), first.Bytes()) || !bytes.Equal(got.Secrets[1].Secret.Bytes(), second.Bytes()) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestListDemoSecretsOrdersOldestFirstAndRespectsTheLimit(t *testing.T) {
	h := start(t)
	receiver := demoBase + "/" + projectA
	h.createWithURL(t, projectA, epID(3), receiver+"?c=3", base.Add(2*time.Second))
	h.createWithURL(t, projectA, epID(1), receiver+"?c=1", base)
	h.createWithURL(t, projectA, epID(2), receiver+"?c=2", base.Add(time.Second))

	all, allErr := h.secrets.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: projectA, ReceiverURL: receiver, Limit: 20})
	two, twoErr := h.secrets.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: projectA, ReceiverURL: receiver, Limit: 2})

	if allErr != nil || twoErr != nil ||
		!slices.Equal(endpointIDsOf(all), []string{epID(1), epID(2), epID(3)}) || !slices.Equal(endpointIDsOf(two), []string{epID(1), epID(2)}) {
		t.Fatalf("all %v (%v), limited %v (%v)", endpointIDsOf(all), allErr, endpointIDsOf(two), twoErr)
	}
}

func TestListDemoSecretsOfAnUnknownProjectIsEmptyNotAnError(t *testing.T) {
	h := start(t)

	got, err := h.secrets.ListDemoSecrets(context.Background(), repository.DemoSecretQuery{ProjectID: "proj_unknown", ReceiverURL: demoBase + "/proj_unknown", Limit: 20})

	if err != nil || got.Secrets == nil || len(got.Secrets) != 0 {
		t.Fatalf("got %#v, %v; want an empty slice", got, err)
	}
}

func TestListURLsSeesTheRowsOfTheCurrentTransaction(t *testing.T) {
	h := start(t)
	urls := repositoryURLLister(h)
	h.createWithURL(t, projectA, epID(1), "https://example.com/committed", base)

	err := h.tx.WithinTx(context.Background(), func(ctx context.Context) error {
		e, entityErr := entity.NewEndpoint(entity.EndpointSpec{ID: epID(2), ProjectID: projectA, URL: "https://example.com/inside", Description: "d"}, base)
		if entityErr != nil {
			return entityErr
		}
		secret, secretErr := entity.NewSigningSecret(bytes.Repeat([]byte{1}, 32))
		if secretErr != nil {
			return secretErr
		}
		if createErr := h.repo.Create(ctx, e, secret); createErr != nil {
			return createErr
		}
		inside, listErr := urls.ListURLs(ctx, projectA)
		if listErr != nil {
			return listErr
		}
		return assertSet(inside, "https://example.com/committed", "https://example.com/inside")
	})
	outside, outsideErr := urls.ListURLs(context.Background(), projectA)

	if err != nil || outsideErr != nil || assertSet(outside, "https://example.com/committed", "https://example.com/inside") != nil {
		t.Fatalf("inside the transaction: %v; after commit: %v, %v", err, outside, outsideErr)
	}
}

func TestListURLsIsScopedToTheProject(t *testing.T) {
	h := start(t)
	h.createWithURL(t, projectA, epID(1), "https://example.com/a", base)
	h.createWithURL(t, projectB, epID(2), "https://example.com/b", base)

	got, err := repositoryURLLister(h).ListURLs(context.Background(), projectA)

	if err != nil || assertSet(got, "https://example.com/a") != nil {
		t.Fatalf("got %v, %v", got, err)
	}
}

func assertSet(got map[string]struct{}, want ...string) error {
	if len(got) != len(want) {
		return errors.New("unexpected number of urls")
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			return errors.New("missing url " + w)
		}
	}
	return nil
}

func (h harness) seedUseCase(max int) *usecase.SeedDemoEndpoints {
	clk := clock.System{}
	return usecase.NewSeedDemoEndpoints(usecase.SeedDemoEndpointsDeps{
		CreateEndpointDeps: usecase.CreateEndpointDeps{
			Endpoints: h.repo, Policy: allowAllPolicy{}, IDs: ids.NewCryptoGenerator(clk), Clock: clk, Random: random.Crypto{}, Tx: h.tx,
		},
		URLs: repositoryURLLister(h),
	}, max)
}

func demoSeedInput(projectID string) usecase.SeedDemoEndpointsInput {
	receiver := demoBase + "/" + projectID
	return usecase.SeedDemoEndpointsInput{ProjectID: projectID, Endpoints: []usecase.DemoEndpointSpec{
		{URL: receiver, Description: "Demo receiver (healthy)"},
		{URL: receiver + "?fail_first=2", Description: "Demo receiver (flaky: fails twice)"},
	}}
}

func TestSeedDemoEndpointsIsIdempotent(t *testing.T) {
	h := start(t)
	uc := h.seedUseCase(20)
	in := demoSeedInput(projectA)

	first, firstErr := uc.Execute(context.Background(), in)
	second, secondErr := uc.Execute(context.Background(), in)

	if firstErr != nil || secondErr != nil || len(first.Created) != 2 || len(second.Created) != 0 || second.Existing != 2 || h.countEndpoints(t, projectA) != 2 {
		t.Fatalf("first %+v (%v), second %+v (%v), rows %d", first, firstErr, second, secondErr, h.countEndpoints(t, projectA))
	}
}

func TestSeedDemoEndpointsLeavesADisabledEndpointAlone(t *testing.T) {
	h := start(t)
	uc := h.seedUseCase(20)
	in := demoSeedInput(projectA)
	if _, err := uc.Execute(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE endpoints SET enabled = false WHERE project_id = $1`, projectA); err != nil {
		t.Fatal(err)
	}

	out, err := uc.Execute(context.Background(), in)

	var enabled int
	if scanErr := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM endpoints WHERE project_id = $1 AND enabled`, projectA).Scan(&enabled); scanErr != nil {
		t.Fatal(scanErr)
	}
	if err != nil || len(out.Created) != 0 || enabled != 0 || h.countEndpoints(t, projectA) != 2 {
		t.Fatalf("out %+v, err %v, enabled rows %d, rows %d", out, err, enabled, h.countEndpoints(t, projectA))
	}
}

func TestSeedDemoEndpointsStaysWithinTheEndpointLimit(t *testing.T) {
	h := start(t)

	_, err := h.seedUseCase(1).Execute(context.Background(), demoSeedInput(projectA))

	if !errors.Is(err, domainerrors.ErrLimitReached) || h.countEndpoints(t, projectA) != 0 {
		t.Fatalf("got %v with %d rows; want the limit error and nothing created", err, h.countEndpoints(t, projectA))
	}
}

func TestConcurrentSeedsCreateExactlyTwoEndpoints(t *testing.T) {
	const runs = 8
	h := start(t)
	uc := h.seedUseCase(20)
	in := demoSeedInput(projectA)
	startGate := make(chan struct{})
	errs := make([]error, runs)
	created := make([]int, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			out, err := uc.Execute(context.Background(), in)
			errs[i], created[i] = err, len(out.Created)
		}()
	}
	close(startGate)
	wg.Wait()

	total := 0
	for _, n := range created {
		total += n
	}
	if err := errors.Join(errs...); err != nil || total != 2 || h.countEndpoints(t, projectA) != 2 {
		t.Fatalf("errors %v, created %d in total, rows %d; want exactly 2", err, total, h.countEndpoints(t, projectA))
	}
}

func TestSeedDemoEndpointsSerializesWithCreateEndpoint(t *testing.T) {
	const limit = 3
	h := start(t)
	seed := h.seedUseCase(limit)
	create := h.createUseCase(limit)
	startGate := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-startGate
		_, _ = seed.Execute(context.Background(), demoSeedInput(projectA)) // may lose the race for the limit; the count is asserted below
	}()
	go func() {
		defer wg.Done()
		<-startGate
		_, _ = create.Execute(context.Background(), usecase.CreateEndpointInput{ProjectID: projectA, URL: "https://example.com/hook"}) // same
	}()
	close(startGate)
	wg.Wait()

	if n := h.countEndpoints(t, projectA); n > limit {
		t.Fatalf("project has %d endpoints, limit %d", n, limit)
	}
}
