package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

var errStorage = errors.New("storage down")

func TestGetOwnedProject(t *testing.T) {
	tests := []struct {
		name    string
		getErr  error
		userID  string
		wantErr error
		wantID  string

		wantNotProvisioned bool
	}{
		{name: "found", userID: "user-1", wantID: "proj_1"},
		{name: "no project yet", userID: "user-2", wantErr: domainerrors.ErrProjectNotProvisioned, wantNotProvisioned: true},
		{name: "storage error is wrapped, not mapped", userID: "user-1", getErr: errStorage, wantErr: errStorage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newOwnerRepo()
			repo.byOwner["user-1"] = entity.Project{ID: "proj_1", OwnerID: "user-1"}
			repo.getErr = tt.getErr

			got, err := usecase.NewGetOwnedProject(repo).Execute(context.Background(), usecase.GetOwnedProjectInput{UserID: tt.userID})

			if !errors.Is(err, tt.wantErr) || got.ID != tt.wantID {
				t.Fatalf("got %+v, %v; want %q, %v", got, err, tt.wantID, tt.wantErr)
			}
			if gotNotProvisioned := errors.Is(err, domainerrors.ErrProjectNotProvisioned); gotNotProvisioned != tt.wantNotProvisioned {
				t.Fatalf("not provisioned = %v, want %v", gotNotProvisioned, tt.wantNotProvisioned)
			}
		})
	}
}
