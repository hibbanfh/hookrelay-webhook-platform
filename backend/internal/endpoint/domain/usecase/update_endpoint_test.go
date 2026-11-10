package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/usecase"
)

func text(s string) *string { return &s }
func flag(b bool) *bool     { return &b }

func TestUpdateEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		projectID  string
		patch      entity.Patch
		getErr     error
		policyErr  error
		updateErr  error
		wantErr    error
		wantField  string
		wantCalls  []string
		wantPolicy []string
	}{
		{name: "url runs the policy", projectID: "proj_1", patch: entity.Patch{URL: text("https://new.example/x")},
			wantCalls: []string{"get", "policy", "update"}, wantPolicy: []string{"https://new.example/x"}},
		{name: "url unchanged still runs the policy", projectID: "proj_1", patch: entity.Patch{URL: text("https://example.com/hook")},
			wantCalls: []string{"get", "policy", "update"}, wantPolicy: []string{"https://example.com/hook"}},
		{name: "disabling makes no DNS lookup", projectID: "proj_1", patch: entity.Patch{Enabled: flag(false)},
			wantCalls: []string{"get", "update"}},
		{name: "description only makes no DNS lookup", projectID: "proj_1", patch: entity.Patch{Description: text("")},
			wantCalls: []string{"get", "update"}},
		{name: "another project endpoint is 404 before any policy", projectID: "proj_2", patch: entity.Patch{URL: text("https://new.example/x")},
			wantErr: domainerrors.ErrNotFound, wantCalls: []string{"get"}},
		{name: "empty patch", projectID: "proj_1", patch: entity.Patch{},
			wantErr: domainerrors.ErrInvalid, wantField: "body", wantCalls: []string{"get"}},
		{name: "invalid url", projectID: "proj_1", patch: entity.Patch{URL: text("ftp://x")},
			wantErr: domainerrors.ErrInvalid, wantField: "url", wantCalls: []string{"get"}},
		{name: "description too long", projectID: "proj_1", patch: entity.Patch{Description: text(strings.Repeat("a", 256))},
			wantErr: domainerrors.ErrInvalid, wantField: "description", wantCalls: []string{"get"}},
		{name: "policy rejects the new url", projectID: "proj_1", patch: entity.Patch{URL: text("https://10.0.0.1/x")},
			policyErr: errors.New("host must be a public address"), wantErr: domainerrors.ErrInvalid, wantField: "url",
			wantCalls: []string{"get", "policy"}, wantPolicy: []string{"https://10.0.0.1/x"}},
		{name: "storage failure on load", projectID: "proj_1", patch: entity.Patch{Enabled: flag(true)},
			getErr: errBoom, wantErr: errBoom, wantCalls: []string{"get"}},
		{name: "storage failure on write", projectID: "proj_1", patch: entity.Patch{Enabled: flag(true)},
			updateErr: errBoom, wantErr: errBoom, wantCalls: []string{"get", "update"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			repo := &fakeEndpoints{rec: rec, rows: newestFirst(1), getErr: tt.getErr, updateErr: tt.updateErr}
			policy := &fakePolicy{rec: rec, err: tt.policyErr}
			uc := usecase.NewUpdateEndpoint(repo, policy, fixedClock{})

			_, err := uc.Execute(context.Background(), usecase.UpdateEndpointInput{ProjectID: tt.projectID, ID: "ep_A", Patch: tt.patch})

			assertError(t, err, tt.wantErr, tt.wantField)
			assertCalls(t, rec.calls, tt.wantCalls)
			assertCalls(t, policy.got, tt.wantPolicy)
		})
	}
}

func TestUpdateEndpointWritesOnlyThePresentFieldsAndMovesUpdatedAt(t *testing.T) {
	rec := &recorder{}
	repo := &fakeEndpoints{rec: rec, rows: newestFirst(1)}
	uc := usecase.NewUpdateEndpoint(repo, &fakePolicy{rec: rec}, fixedClock{})

	got, err := uc.Execute(context.Background(), usecase.UpdateEndpointInput{ProjectID: "proj_1", ID: "ep_A", Patch: entity.Patch{Enabled: flag(false)}})

	if err != nil || repo.gotPatch.URL != nil || repo.gotPatch.Description != nil || repo.gotPatch.Enabled == nil ||
		!repo.gotAt.Equal(fixedNow) || got.Enabled || !got.UpdatedAt.Equal(fixedNow) || got.URL != "https://example.com/hook" {
		t.Fatalf("patch %+v at %v, got %+v, err %v", repo.gotPatch, repo.gotAt, got, err)
	}
}
