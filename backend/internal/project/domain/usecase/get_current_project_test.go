package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

func TestGetCurrentProject(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name    string
		id      string
		getErr  error
		wantErr error
	}{
		{name: "found", id: "proj_1"},
		{name: "not found", id: "proj_missing", wantErr: domainerrors.ErrNotFound},
		{name: "storage failure", id: "proj_1", getErr: errBoom, wantErr: errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.projects["proj_1"] = entity.Project{ID: "proj_1", Name: "Acme", CreatedAt: fixedNow}
			repo.getErr = tt.getErr

			p, err := usecase.NewGetCurrentProject(repo).Execute(context.Background(), usecase.GetCurrentProjectInput{ProjectID: tt.id})

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && p.Name != "Acme" {
				t.Fatalf("got %+v", p)
			}
		})
	}
}
