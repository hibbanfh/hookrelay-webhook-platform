package usecase

import (
	"context"
	"fmt"
	"maps"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
)

// SeedDemoEndpointsDeps are the collaborators of SeedDemoEndpoints: those of CreateEndpoint plus
// the reader of the URLs a project already has.
type SeedDemoEndpointsDeps struct {
	CreateEndpointDeps
	URLs repository.EndpointURLLister
}

// SeedDemoEndpoints makes sure a project has the demo endpoints it is given, by exact URL
// (hookrelay seed). It is internal: no HTTP route exposes it (ADR 0017 section 5).
type SeedDemoEndpoints struct {
	deps          SeedDemoEndpointsDeps
	maxPerProject int
}

// NewSeedDemoEndpoints builds the use case. maxPerProject mirrors MAX_ENDPOINTS_PER_PROJECT.
func NewSeedDemoEndpoints(deps SeedDemoEndpointsDeps, maxPerProject int) *SeedDemoEndpoints {
	return &SeedDemoEndpoints{deps: deps, maxPerProject: maxPerProject}
}

// DemoEndpointSpec is one endpoint the seed wants to exist.
type DemoEndpointSpec struct{ URL, Description string }

// SeedDemoEndpointsInput names the project and the endpoints it should have.
type SeedDemoEndpointsInput struct {
	ProjectID string
	Endpoints []DemoEndpointSpec
}

// SeedDemoEndpointsOutput reports what the run did: the endpoints it created, and how many of the
// wanted URLs already existed.
type SeedDemoEndpointsOutput struct {
	Created  []entity.Endpoint
	Existing int
}

// plannedEndpoint is an endpoint that is valid and allowed, with its secret, but not stored yet.
type plannedEndpoint struct {
	endpoint entity.Endpoint
	secret   entity.SigningSecret
}

// Execute validates and checks every wanted endpoint first, so DNS and key generation happen
// before the transaction and the project lock is held briefly. Then, under that lock, it creates
// the ones whose URL is missing. The lock is the one CreateEndpoint takes, so a second seed and a
// concurrent create take turns and nothing is created twice.
func (u *SeedDemoEndpoints) Execute(ctx context.Context, in SeedDemoEndpointsInput) (SeedDemoEndpointsOutput, error) {
	planned, err := u.plan(ctx, in)
	if err != nil {
		return SeedDemoEndpointsOutput{}, err
	}
	var out SeedDemoEndpointsOutput
	err = u.deps.Tx.WithinTx(ctx, func(ctx context.Context) error {
		var storeErr error
		out, storeErr = u.store(ctx, in.ProjectID, planned)
		return storeErr
	})
	if err != nil {
		return SeedDemoEndpointsOutput{}, fmt.Errorf("seed demo endpoints of project %s: %w", in.ProjectID, err)
	}
	return out, nil
}

func (u *SeedDemoEndpoints) plan(ctx context.Context, in SeedDemoEndpointsInput) ([]plannedEndpoint, error) {
	planned := make([]plannedEndpoint, 0, len(in.Endpoints))
	for _, spec := range in.Endpoints {
		p, err := u.planOne(ctx, in.ProjectID, spec)
		if err != nil {
			return nil, err
		}
		planned = append(planned, p)
	}
	return planned, nil
}

func (u *SeedDemoEndpoints) planOne(ctx context.Context, projectID string, spec DemoEndpointSpec) (plannedEndpoint, error) {
	e, err := entity.NewEndpoint(entity.EndpointSpec{
		ID: u.deps.IDs.New(entity.EndpointIDPrefix), ProjectID: projectID, URL: spec.URL, Description: spec.Description,
	}, u.deps.Clock.Now())
	if err != nil {
		return plannedEndpoint{}, err
	}
	if err := checkURL(ctx, u.deps.Policy, e.URL); err != nil {
		return plannedEndpoint{}, err
	}
	secret, err := newSigningSecret(u.deps.Random)
	if err != nil {
		return plannedEndpoint{}, err
	}
	return plannedEndpoint{endpoint: e, secret: secret}, nil
}

// store runs inside the transaction. The count locks the project row; the URLs are read after it.
func (u *SeedDemoEndpoints) store(ctx context.Context, projectID string, planned []plannedEndpoint) (SeedDemoEndpointsOutput, error) {
	count, err := u.deps.Endpoints.CountForUpdate(ctx, projectID)
	if err != nil {
		return SeedDemoEndpointsOutput{}, fmt.Errorf("count endpoints: %w", err)
	}
	urls, err := u.deps.URLs.ListURLs(ctx, projectID)
	if err != nil {
		return SeedDemoEndpointsOutput{}, fmt.Errorf("list endpoint urls: %w", err)
	}
	missing := withoutExisting(planned, urls)
	if count+len(missing) > u.maxPerProject {
		return SeedDemoEndpointsOutput{}, &domainerrors.LimitReachedError{Limit: u.maxPerProject}
	}
	created, err := u.createAll(ctx, missing)
	if err != nil {
		return SeedDemoEndpointsOutput{}, err
	}
	return SeedDemoEndpointsOutput{Created: created, Existing: len(planned) - len(missing)}, nil
}

func (u *SeedDemoEndpoints) createAll(ctx context.Context, missing []plannedEndpoint) ([]entity.Endpoint, error) {
	created := make([]entity.Endpoint, 0, len(missing))
	for i := range missing {
		p := &missing[i]
		if err := u.deps.Endpoints.Create(ctx, p.endpoint, p.secret); err != nil {
			return nil, fmt.Errorf("create endpoint %s: %w", p.endpoint.ID, err)
		}
		created = append(created, p.endpoint)
	}
	return created, nil
}

// withoutExisting keeps the planned endpoints whose URL no endpoint has yet. A URL that appears
// twice in the plan is kept once.
func withoutExisting(planned []plannedEndpoint, existing map[string]struct{}) []plannedEndpoint {
	seen := make(map[string]struct{}, len(existing)+len(planned))
	maps.Copy(seen, existing)
	missing := make([]plannedEndpoint, 0, len(planned))
	for i := range planned {
		url := planned[i].endpoint.URL
		_, known := seen[url]
		if known {
			continue
		}
		seen[url] = struct{}{}
		missing = append(missing, planned[i])
	}
	return missing
}
