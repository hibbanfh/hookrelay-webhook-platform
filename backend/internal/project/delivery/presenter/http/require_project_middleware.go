package httppresenter

import (
	"context"
	"net/http"

	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

const (
	// SessionHeader carries the dashboard session from the Next.js proxy to this API.
	SessionHeader = "X-Hookrelay-Session"

	authorizationHeader = "Authorization"
)

type sessionAuthenticator interface {
	Execute(ctx context.Context, in usecase.AuthenticateSessionInput) (string, error)
}

type ownedProjectGetter interface {
	Execute(ctx context.Context, in usecase.GetOwnedProjectInput) (entity.Project, error)
}

// ProjectGuardDeps are the use cases RequireProject needs.
type ProjectGuardDeps struct {
	Keys     keyAuthenticator
	Sessions sessionAuthenticator
	Owned    ownedProjectGetter
}

// RequireProject authenticates a request with exactly one credential: an API key
// ("Authorization: Bearer hr_...") or a dashboard session (X-Hookrelay-Session). It stores the
// project ID in the context. No credential, both credentials, and every bad credential produce
// the same 401 response; the identity provider is only asked when a well-formed session header
// is present.
func RequireProject(deps ProjectGuardDeps, resp httpx.Responder) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			projectID, err := deps.resolveProject(r)
			if err != nil {
				resp.Error(w, r, toAPIError(err))
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithProjectID(r.Context(), projectID)))
		})
	}
}

func (d ProjectGuardDeps) resolveProject(r *http.Request) (string, error) {
	authz, sessionToken := r.Header.Get(authorizationHeader), r.Header.Get(SessionHeader)
	hasKey, hasSession := authz != "", sessionToken != ""
	if hasKey == hasSession {
		return "", domainerrors.ErrUnauthorized
	}
	if hasKey {
		return d.projectFromKey(r.Context(), authz)
	}
	return d.projectFromSession(r.Context(), sessionToken)
}

func (d ProjectGuardDeps) projectFromKey(ctx context.Context, authorization string) (string, error) {
	token, ok := bearerToken(authorization)
	if !ok {
		return "", domainerrors.ErrUnauthorized
	}
	return d.Keys.Execute(ctx, usecase.AuthenticateAPIKeyInput{Token: token})
}

func (d ProjectGuardDeps) projectFromSession(ctx context.Context, sessionToken string) (string, error) {
	userID, err := d.Sessions.Execute(ctx, usecase.AuthenticateSessionInput{Token: sessionToken})
	if err != nil {
		return "", err
	}
	project, err := d.Owned.Execute(ctx, usecase.GetOwnedProjectInput{UserID: userID})
	if err != nil {
		return "", err
	}
	return project.ID, nil
}
