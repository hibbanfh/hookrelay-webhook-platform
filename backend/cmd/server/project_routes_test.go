package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hookrelay/internal/platform/config"
	"hookrelay/internal/project/data/neonauth"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

type testClock struct{}

func (testClock) Now() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) }

func TestNewSessionVerifierWithoutNeonRejectsEverySession(t *testing.T) {
	verifier := newSessionVerifier(config.Config{}, testClock{})
	token, err := entity.ParseSessionToken("tok")
	if err != nil {
		t.Fatal(err)
	}

	_, err = verifier.Verify(context.Background(), token)

	if _, isDisabled := verifier.(neonauth.Disabled); !isDisabled || !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("verifier %T, err %v", verifier, err)
	}
}

func TestNewSessionVerifierDoesNotFollowRedirectsAndFailsClosed(t *testing.T) {
	redirected := false
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer other.Close()
	neon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer neon.Close()
	cfg := config.Config{
		NeonAuthBaseURL: neon.URL, NeonAuthSessionCookie: "c", DashboardOrigin: "http://localhost:3000",
		NeonAuthTimeout: time.Second, SessionCacheTTL: 5 * time.Second,
	}
	token, err := entity.ParseSessionToken("tok")
	if err != nil {
		t.Fatal(err)
	}

	_, err = newSessionVerifier(cfg, testClock{}).Verify(context.Background(), token)

	if !errors.Is(err, domainerrors.ErrAuthUnavailable) || redirected {
		t.Fatalf("err %v, redirect followed = %v", err, redirected)
	}
}
