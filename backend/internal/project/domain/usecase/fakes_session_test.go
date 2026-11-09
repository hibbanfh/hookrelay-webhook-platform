package usecase_test

import (
	"context"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

// fakeVerifier is a repository.SessionVerifier that counts its calls.
type fakeVerifier struct {
	user  entity.SessionUser
	err   error
	calls int
}

func (f *fakeVerifier) Verify(context.Context, entity.SessionToken) (entity.SessionUser, error) {
	f.calls++
	return f.user, f.err
}

// ownerRepo is an in-memory ProjectRepository + OwnedProjectRepository with one project per owner.
type ownerRepo struct {
	byOwner map[string]entity.Project
	keys    []entity.APIKey

	createErr error
	getErr    error
	// racer is stored just before CreateWithKey reports a conflict: a concurrent writer won.
	racer *entity.Project
	// getErrAfterCreate fails GetByOwner once CreateWithKey has been attempted.
	getErrAfterCreate error
	created           bool
}

func newOwnerRepo() *ownerRepo { return &ownerRepo{byOwner: map[string]entity.Project{}} }

func (r *ownerRepo) CreateWithKey(_ context.Context, p entity.Project, k entity.APIKey) error {
	r.created = true
	if r.racer != nil {
		r.byOwner[r.racer.OwnerID] = *r.racer
	}
	if r.createErr != nil {
		return r.createErr
	}
	r.byOwner[p.OwnerID] = p
	r.keys = append(r.keys, k)
	return nil
}

func (r *ownerRepo) GetByID(context.Context, string) (entity.Project, error) {
	return entity.Project{}, domainerrors.ErrNotFound
}

func (r *ownerRepo) GetByKeyHash(context.Context, entity.KeyHash) (entity.Project, error) {
	return entity.Project{}, domainerrors.ErrNotFound
}

func (r *ownerRepo) GetByOwner(_ context.Context, ownerID string) (entity.Project, error) {
	if r.getErr != nil {
		return entity.Project{}, r.getErr
	}
	if r.created && r.getErrAfterCreate != nil {
		return entity.Project{}, r.getErrAfterCreate
	}
	p, ok := r.byOwner[ownerID]
	if !ok {
		return entity.Project{}, domainerrors.ErrNotFound
	}
	return p, nil
}
