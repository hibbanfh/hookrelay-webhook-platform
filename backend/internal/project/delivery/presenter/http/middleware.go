package httppresenter

import (
	"context"
	"net/http"
	"strings"

	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

const bearerScheme = "Bearer"

type keyAuthenticator interface {
	Execute(ctx context.Context, in usecase.AuthenticateAPIKeyInput) (string, error)
}

// RequireAPIKey authenticates "Authorization: Bearer hr_..." and stores the project ID in the
// context. A missing header, a wrong scheme, a malformed key and an unknown key all produce
// the same 401 response.
func RequireAPIKey(authn keyAuthenticator, resp httpx.Responder) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				resp.Error(w, r, toAPIError(domainerrors.ErrUnauthorized))
				return
			}
			projectID, err := authn.Execute(r.Context(), usecase.AuthenticateAPIKeyInput{Token: token})
			if err != nil {
				resp.Error(w, r, toAPIError(err))
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithProjectID(r.Context(), projectID)))
		})
	}
}

// bearerToken extracts the token from a "Bearer <token>" header value.
func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	token = strings.TrimSpace(token)
	return token, found && strings.EqualFold(scheme, bearerScheme) && token != ""
}
