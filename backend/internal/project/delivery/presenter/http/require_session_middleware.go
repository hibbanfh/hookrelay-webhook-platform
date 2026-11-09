package httppresenter

import (
	"net/http"

	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

// RequireSession authenticates a dashboard session only and stores the user ID in the context.
// An Authorization header next to the session is rejected, like in RequireProject.
func RequireSession(sessions sessionAuthenticator, resp httpx.Responder) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := userFromSession(r, sessions)
			if err != nil {
				resp.Error(w, r, toAPIError(err))
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUserID(r.Context(), userID)))
		})
	}
}

func userFromSession(r *http.Request, sessions sessionAuthenticator) (string, error) {
	sessionToken := r.Header.Get(SessionHeader)
	if sessionToken == "" || r.Header.Get(authorizationHeader) != "" {
		return "", domainerrors.ErrUnauthorized
	}
	return sessions.Execute(r.Context(), usecase.AuthenticateSessionInput{Token: sessionToken})
}
