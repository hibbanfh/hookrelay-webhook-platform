package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

func TestAuthenticateSession(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		verifier   *fakeVerifier
		wantUser   string
		wantErr    error
		wantCalled int
	}{
		{name: "valid", token: "tok", verifier: &fakeVerifier{user: entity.SessionUser{ID: "user-1"}}, wantUser: "user-1", wantCalled: 1},
		{name: "empty token never reaches the verifier", token: "", verifier: &fakeVerifier{}, wantErr: domainerrors.ErrUnauthorized},
		{name: "malformed token never reaches the verifier", token: "a b", verifier: &fakeVerifier{}, wantErr: domainerrors.ErrUnauthorized},
		{name: "oversized token never reaches the verifier", token: strings.Repeat("a", 2049), verifier: &fakeVerifier{}, wantErr: domainerrors.ErrUnauthorized},
		{name: "unknown session", token: "tok", verifier: &fakeVerifier{err: domainerrors.ErrUnauthorized}, wantErr: domainerrors.ErrUnauthorized, wantCalled: 1},
		{name: "provider down", token: "tok", verifier: &fakeVerifier{err: domainerrors.ErrAuthUnavailable}, wantErr: domainerrors.ErrAuthUnavailable, wantCalled: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := usecase.NewAuthenticateSession(tt.verifier)

			got, err := uc.Execute(context.Background(), usecase.AuthenticateSessionInput{Token: tt.token})

			if !errors.Is(err, tt.wantErr) || got != tt.wantUser || tt.verifier.calls != tt.wantCalled {
				t.Fatalf("got user %q err %v calls %d, want %q %v %d", got, err, tt.verifier.calls, tt.wantUser, tt.wantErr, tt.wantCalled)
			}
		})
	}
}
