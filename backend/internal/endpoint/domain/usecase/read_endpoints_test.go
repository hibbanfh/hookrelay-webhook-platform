package usecase_test

import (
	"context"
	"testing"
	"time"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
	"hookrelay/internal/endpoint/domain/usecase"
)

// newestFirst builds n endpoints of proj_1, newest first, one minute apart.
func newestFirst(n int) []entity.Endpoint {
	rows := make([]entity.Endpoint, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, endpointFixture("ep_"+string(rune('A'+i)), "proj_1", fixedNow.Add(-time.Duration(i)*time.Minute)))
	}
	return rows
}

func TestListEndpoints(t *testing.T) {
	anchor := &entity.PageAnchor{CreatedAt: fixedNow, ID: "ep_prev"}
	tests := []struct {
		name         string
		rows         int
		limit        int
		after        *entity.PageAnchor
		listErr      error
		wantErr      error
		wantField    string
		wantLen      int
		wantNextID   string
		wantAskLimit int
	}{
		{name: "fewer rows than the limit has no next page", rows: 3, limit: 5, wantLen: 3, wantAskLimit: 6},
		{name: "exactly the limit has no next page", rows: 5, limit: 5, wantLen: 5, wantAskLimit: 6},
		{name: "one more than the limit has a next page", rows: 6, limit: 5, wantLen: 5, wantNextID: "ep_E", wantAskLimit: 6},
		{name: "empty project", rows: 0, limit: 20, wantLen: 0, wantAskLimit: 21},
		{name: "cursor is passed through", rows: 2, limit: 5, after: anchor, wantLen: 2, wantAskLimit: 6},
		{name: "limit below 1", rows: 3, limit: 0, wantErr: domainerrors.ErrInvalid, wantField: "limit"},
		{name: "storage failure", rows: 3, limit: 5, listErr: errBoom, wantErr: errBoom, wantAskLimit: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeEndpoints{rec: &recorder{}, rows: newestFirst(tt.rows), listErr: tt.listErr}

			out, err := usecase.NewListEndpoints(repo).Execute(context.Background(),
				usecase.ListEndpointsInput{ProjectID: "proj_1", Limit: tt.limit, After: tt.after})

			assertError(t, err, tt.wantErr, tt.wantField)
			assertList(t, out, repo.gotQuery, tt.wantLen, tt.wantNextID, tt.wantAskLimit, tt.after)
		})
	}
}

func assertList(t *testing.T, out usecase.ListEndpointsOutput, q repository.ListQuery, wantLen int, wantNextID string, wantAskLimit int, wantAfter *entity.PageAnchor) {
	t.Helper()
	nextID := ""
	if out.Next != nil {
		nextID = out.Next.ID
	}
	if len(out.Endpoints) != wantLen || nextID != wantNextID || q.Limit != wantAskLimit || q.After != wantAfter {
		t.Fatalf("page %d items, next %q, asked limit %d, after %v", len(out.Endpoints), nextID, q.Limit, q.After)
	}
}

func TestListEndpointsNextAnchorIsTheLastItemOfThePage(t *testing.T) {
	repo := &fakeEndpoints{rec: &recorder{}, rows: newestFirst(4)}

	out, err := usecase.NewListEndpoints(repo).Execute(context.Background(), usecase.ListEndpointsInput{ProjectID: "proj_1", Limit: 3})

	last := repo.rows[2]
	if err != nil || out.Next == nil || out.Next.ID != last.ID || !out.Next.CreatedAt.Equal(last.CreatedAt) {
		t.Fatalf("next %+v, err %v", out.Next, err)
	}
}

type fakeSecrets struct {
	secret entity.SigningSecret
	err    error
	gotID  string
}

func (f *fakeSecrets) GetSecret(_ context.Context, _, id string) (entity.SigningSecret, error) {
	f.gotID = id
	return f.secret, f.err
}

func TestGetEndpointReturnsTheOwnEndpoint(t *testing.T) {
	repo := &fakeEndpoints{rec: &recorder{}, rows: newestFirst(1)}

	got, err := usecase.NewGetEndpoint(repo).Execute(context.Background(), usecase.GetEndpointInput{ProjectID: "proj_1", ID: "ep_A"})

	if err != nil || got.ID != "ep_A" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestGetEndpointFails(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		id        string
		getErr    error
		wantErr   error
	}{
		{name: "another project endpoint is not found", projectID: "proj_2", id: "ep_A", wantErr: domainerrors.ErrNotFound},
		{name: "unknown id", projectID: "proj_1", id: "ep_missing", wantErr: domainerrors.ErrNotFound},
		{name: "storage failure", projectID: "proj_1", id: "ep_A", getErr: errBoom, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeEndpoints{rec: &recorder{}, rows: newestFirst(1), getErr: tt.getErr}

			_, err := usecase.NewGetEndpoint(repo).Execute(context.Background(), usecase.GetEndpointInput{ProjectID: tt.projectID, ID: tt.id})

			assertError(t, err, tt.wantErr, "")
		})
	}
}

func TestGetEndpointSecretReturnsTheSecretOfTheRequestedEndpoint(t *testing.T) {
	secret, err := entity.NewSigningSecret(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeSecrets{secret: secret}

	got, err := usecase.NewGetEndpointSecret(repo).Execute(context.Background(), usecase.GetEndpointSecretInput{ProjectID: "proj_1", ID: "ep_A"})

	if err != nil || got.Value() != secret.Value() || repo.gotID != "ep_A" {
		t.Fatalf("got %v, %v, asked for %q", got, err, repo.gotID)
	}
}

func TestGetEndpointSecretFails(t *testing.T) {
	tests := []struct {
		name    string
		repoErr error
	}{
		{name: "not found", repoErr: domainerrors.ErrNotFound},
		{name: "storage or decryption failure", repoErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeSecrets{err: tt.repoErr}

			_, err := usecase.NewGetEndpointSecret(repo).Execute(context.Background(), usecase.GetEndpointSecretInput{ProjectID: "proj_1", ID: "ep_A"})

			assertError(t, err, tt.repoErr, "")
		})
	}
}
