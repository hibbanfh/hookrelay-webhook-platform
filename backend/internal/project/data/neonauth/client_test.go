package neonauth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/project/data/neonauth"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

const (
	testCookieName = "__Secure-neon-auth.session_token"
	testOrigin     = "http://localhost:3000"
	testUserID     = "0b1f9c3e-1111-4222-8333-444455556666"
	testTokenValue = "s3cr3t-token-value.sig%3D"
	validBody      = `{"session":{"expiresAt":"2026-11-16T10:00:00.000Z","userId":"` + testUserID + `"},"user":{"id":"` + testUserID + `","email":"a@example.com"}}`
)

func token(t *testing.T) entity.SessionToken {
	t.Helper()
	tok, err := entity.ParseSessionToken(testTokenValue)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func noRedirects() *http.Client {
	return &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func newClient(srv *httptest.Server, hc *http.Client) *neonauth.Client {
	return neonauth.NewClient(neonauth.Options{HTTP: hc, BaseURL: srv.URL + "/neondb/auth/", CookieName: testCookieName, Origin: testOrigin})
}

func respondWith(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestVerifyMapsNeonResponses(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
		wantID  string
	}{
		{name: "valid session", handler: respondWith(200, validBody), wantID: testUserID},
		{name: "200 null means no session", handler: respondWith(200, "null"), wantErr: domainerrors.ErrUnauthorized},
		{name: "401 means no session", handler: respondWith(401, `{"message":"x"}`), wantErr: domainerrors.ErrUnauthorized},
		{name: "500 fails closed", handler: respondWith(500, "boom"), wantErr: domainerrors.ErrAuthUnavailable},
		{name: "503 fails closed", handler: respondWith(503, ""), wantErr: domainerrors.ErrAuthUnavailable},
		{name: "403 fails closed", handler: respondWith(403, `{"code":"INVALID_ORIGIN"}`), wantErr: domainerrors.ErrAuthUnavailable},
		{name: "429 fails closed", handler: respondWith(429, ""), wantErr: domainerrors.ErrAuthUnavailable},
		{name: "redirect is not followed and fails closed", handler: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}, wantErr: domainerrors.ErrAuthUnavailable},
		{name: "bad json fails closed", handler: respondWith(200, "{not json"), wantErr: domainerrors.ErrAuthUnavailable},
		{name: "empty body fails closed", handler: respondWith(200, ""), wantErr: domainerrors.ErrAuthUnavailable},
		{name: "object without user fails closed", handler: respondWith(200, `{"session":{}}`), wantErr: domainerrors.ErrAuthUnavailable},
		{name: "non uuid id fails closed", handler: respondWith(200, `{"user":{"id":"not-a-uuid"}}`), wantErr: domainerrors.ErrAuthUnavailable},
		{name: "body over 64KB fails closed", handler: respondWith(200, `{"pad":"`+strings.Repeat("a", 70_000)+`","user":{"id":"`+testUserID+`"}}`), wantErr: domainerrors.ErrAuthUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			user, err := newClient(srv, noRedirects()).Verify(context.Background(), token(t))

			if !errors.Is(err, tt.wantErr) || user.ID != tt.wantID {
				t.Fatalf("got %+v, %v; want id %q err %v", user, err, tt.wantID, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), testTokenValue) {
				t.Fatalf("the error leaks the token: %v", err)
			}
		})
	}
}

func TestVerifyReturnsTheSessionExpiry(t *testing.T) {
	srv := httptest.NewServer(respondWith(200, validBody))
	defer srv.Close()

	user, err := newClient(srv, noRedirects()).Verify(context.Background(), token(t))

	want := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	if err != nil || !user.ExpiresAt.Equal(want) {
		t.Fatalf("got %+v, %v; want expiry %v", user, err, want)
	}
}

func TestVerifySendsCookieOriginAndAcceptToGetSession(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		_, _ = w.Write([]byte(validBody))
	}))
	defer srv.Close()

	if _, err := newClient(srv, noRedirects()).Verify(context.Background(), token(t)); err != nil {
		t.Fatal(err)
	}

	checks := map[string][2]string{
		"method": {got.Method, http.MethodGet},
		"path":   {got.URL.Path, "/neondb/auth/get-session"},
		"cookie": {got.Header.Get("Cookie"), testCookieName + "=" + testTokenValue},
		"origin": {got.Header.Get("Origin"), testOrigin},
		"accept": {got.Header.Get("Accept"), "application/json"},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %q, want %q", name, c[0], c[1])
		}
	}
}

func TestVerifyTimeoutFailsClosed(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	hc := noRedirects()
	hc.Timeout = 50 * time.Millisecond

	_, err := newClient(srv, hc).Verify(context.Background(), token(t))

	if !errors.Is(err, domainerrors.ErrAuthUnavailable) {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), testTokenValue) {
		t.Fatalf("the error leaks the token: %v", err)
	}
}

func TestVerifyNetworkErrorFailsClosed(t *testing.T) {
	srv := httptest.NewServer(respondWith(200, validBody))
	client := newClient(srv, noRedirects())
	srv.Close()

	_, err := client.Verify(context.Background(), token(t))

	if !errors.Is(err, domainerrors.ErrAuthUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyHonoursContextCancellation(t *testing.T) {
	srv := httptest.NewServer(respondWith(200, validBody))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newClient(srv, noRedirects()).Verify(ctx, token(t))

	if !errors.Is(err, domainerrors.ErrAuthUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestDisabledAlwaysRejects(t *testing.T) {
	_, err := neonauth.Disabled{}.Verify(context.Background(), token(t))

	if !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
}
