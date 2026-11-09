package repositoryimpl_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/project/data/model"
	repositoryimpl "hookrelay/internal/project/data/repository"
	"hookrelay/internal/project/data/source"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/repository"
)

// fakeSource is a ProjectPersistent that records its inputs and returns canned results.
type fakeSource struct {
	err        error
	row        model.Project
	gotProject model.Project
	gotKey     model.APIKey
	gotID      string
	gotHash    []byte
}

func (f *fakeSource) CreateWithKey(_ context.Context, p model.Project, k model.APIKey) error {
	f.gotProject, f.gotKey = p, k
	return f.err
}

func (f *fakeSource) GetByID(_ context.Context, id string) (model.Project, error) {
	f.gotID = id
	return f.row, f.err
}

func (f *fakeSource) GetByKeyHash(_ context.Context, hash []byte) (model.Project, error) {
	f.gotHash = hash
	return f.row, f.err
}

// outcome is the classification of an error that tests compare as one value.
type outcome struct{ failed, conflict, notFound, wrapsCause bool }

func classify(err, cause error) outcome {
	return outcome{
		failed:     err != nil,
		conflict:   errors.Is(err, domainerrors.ErrConflict),
		notFound:   errors.Is(err, domainerrors.ErrNotFound),
		wrapsCause: cause != nil && errors.Is(err, cause),
	}
}

var (
	errBoom  = errors.New("boom")
	testHash = entity.KeyHash{1, 2, 3}
	testTime = time.Date(2026, 11, 8, 12, 0, 0, 0, time.FixedZone("x", 3600))
)

func TestCreateWithKeyMapsEntitiesToModels(t *testing.T) {
	src := &fakeSource{}
	p := entity.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime}
	k := entity.APIKey{ID: "key_1", ProjectID: "proj_1", Prefix: "ABCDEFGH", Hash: testHash, CreatedAt: testTime}

	err := repositoryimpl.NewProjectRepository(src).CreateWithKey(context.Background(), p, k)

	wantProject := model.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime}
	if err != nil || src.gotProject != wantProject {
		t.Fatalf("err %v, project %+v, want %+v", err, src.gotProject, wantProject)
	}
	gotKey := src.gotKey
	if gotKey.ID != "key_1" || gotKey.ProjectID != "proj_1" || gotKey.Prefix != "ABCDEFGH" ||
		string(gotKey.Hash) != string(testHash[:]) || !gotKey.CreatedAt.Equal(testTime) {
		t.Fatalf("key model %+v", gotKey)
	}
}

func TestCreateWithKeyTranslatesSourceErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		cause error
		want  outcome
	}{
		{name: "ok"},
		{name: "unique violation becomes conflict", err: source.ErrUniqueViolation, want: outcome{failed: true, conflict: true}},
		{name: "unknown error is wrapped", err: errBoom, cause: errBoom, want: outcome{failed: true, wrapsCause: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := repositoryimpl.NewProjectRepository(&fakeSource{err: tt.err})

			err := repo.CreateWithKey(context.Background(), entity.Project{ID: "proj_1"}, entity.APIKey{ID: "key_1"})

			if got := classify(err, tt.cause); got != tt.want {
				t.Fatalf("err = %v, got %+v, want %+v", err, got, tt.want)
			}
		})
	}
}

func lookupByID(r repository.ProjectRepository) (entity.Project, error) {
	return r.GetByID(context.Background(), "proj_1")
}

func lookupByKeyHash(r repository.ProjectRepository) (entity.Project, error) {
	return r.GetByKeyHash(context.Background(), testHash)
}

func TestLookupsPassTheKeyAndReturnUTC(t *testing.T) {
	tests := []struct {
		name string
		call func(repository.ProjectRepository) (entity.Project, error)
		want func(*fakeSource) bool
	}{
		{"by id", lookupByID, func(s *fakeSource) bool { return s.gotID == "proj_1" }},
		{"by key hash", lookupByKeyHash, func(s *fakeSource) bool { return string(s.gotHash) == string(testHash[:]) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeSource{row: model.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime}}

			got, err := tt.call(repositoryimpl.NewProjectRepository(src))

			want := entity.Project{ID: "proj_1", Name: "Acme", CreatedAt: testTime.UTC()}
			if err != nil || got != want || got.CreatedAt.Location() != time.UTC {
				t.Fatalf("got %+v (%v), want %+v", got, err, want)
			}
			if !tt.want(src) {
				t.Fatalf("source got id %q hash %v", src.gotID, src.gotHash)
			}
		})
	}
}

func TestLookupsTranslateSourceErrors(t *testing.T) {
	tests := []struct {
		name  string
		call  func(repository.ProjectRepository) (entity.Project, error)
		err   error
		cause error
		want  outcome
	}{
		{"by id: not found", lookupByID, source.ErrNotFound, nil, outcome{failed: true, notFound: true}},
		{"by id: other errors are wrapped, never not found", lookupByID, errBoom, errBoom, outcome{failed: true, wrapsCause: true}},
		{"by key hash: not found", lookupByKeyHash, source.ErrNotFound, nil, outcome{failed: true, notFound: true}},
		{"by key hash: other errors are wrapped, never not found", lookupByKeyHash, errBoom, errBoom, outcome{failed: true, wrapsCause: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := repositoryimpl.NewProjectRepository(&fakeSource{err: tt.err})

			_, err := tt.call(repo)

			if got := classify(err, tt.cause); got != tt.want {
				t.Fatalf("err = %v, got %+v, want %+v", err, got, tt.want)
			}
		})
	}
}

func TestCreateWithKeyPassesRevokedAtThrough(t *testing.T) {
	revoked := testTime.Add(time.Hour)
	src := &fakeSource{}

	err := repositoryimpl.NewProjectRepository(src).CreateWithKey(context.Background(),
		entity.Project{ID: "proj_1"}, entity.APIKey{ID: "key_1", RevokedAt: &revoked})

	if err != nil || src.gotKey.RevokedAt == nil || !src.gotKey.RevokedAt.Equal(revoked) {
		t.Fatalf("err %v, key %+v", err, src.gotKey)
	}
}
