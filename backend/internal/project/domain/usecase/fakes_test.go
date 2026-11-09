package usecase_test

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

var fixedNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

// seqIDs returns <prefix>1, <prefix>2, ... per prefix.
type seqIDs struct{ n map[string]int }

func (s *seqIDs) New(prefix string) string {
	if s.n == nil {
		s.n = map[string]int{}
	}
	s.n[prefix]++
	return fmt.Sprintf("%s%d", prefix, s.n[prefix])
}

type fakeRandom struct {
	fill byte
	err  error
}

func (f fakeRandom) Bytes(n int) ([]byte, error) {
	return bytes.Repeat([]byte{f.fill}, n), f.err
}

// fakeRepo is an in-memory repository.ProjectRepository that can be told to fail.
type fakeRepo struct {
	projects map[string]entity.Project
	byHash   map[entity.KeyHash]string
	keys     []entity.APIKey

	createErr error
	getErr    error
	hashErr   error
	// winnerOnCreateErr simulates losing a race: when createErr is returned, a concurrent
	// writer's project already owns the key.
	winnerOnCreateErr bool
	// hashErrAfterConflict is returned by GetByKeyHash once CreateWithKey has failed.
	hashErrAfterConflict error
	createFailed         bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{projects: map[string]entity.Project{}, byHash: map[entity.KeyHash]string{}}
}

func (f *fakeRepo) CreateWithKey(_ context.Context, p entity.Project, k entity.APIKey) error {
	if f.createErr != nil && f.winnerOnCreateErr {
		f.projects["proj_winner"] = entity.Project{ID: "proj_winner", Name: "Winner", CreatedAt: fixedNow}
		f.byHash[k.Hash] = "proj_winner"
	}
	if f.createErr != nil {
		f.createFailed = true
		return f.createErr
	}
	f.projects[p.ID] = p
	f.byHash[k.Hash] = p.ID
	f.keys = append(f.keys, k)
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, id string) (entity.Project, error) {
	if f.getErr != nil {
		return entity.Project{}, f.getErr
	}
	p, ok := f.projects[id]
	if !ok {
		return entity.Project{}, domainerrors.ErrNotFound
	}
	return p, nil
}

func (f *fakeRepo) GetByKeyHash(_ context.Context, h entity.KeyHash) (entity.Project, error) {
	if f.hashErr != nil {
		return entity.Project{}, f.hashErr
	}
	if f.createFailed && f.hashErrAfterConflict != nil {
		return entity.Project{}, f.hashErrAfterConflict
	}
	id, ok := f.byHash[h]
	if !ok {
		return entity.Project{}, domainerrors.ErrNotFound
	}
	return f.projects[id], nil
}
