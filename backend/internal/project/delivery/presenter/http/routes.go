package httppresenter

import "net/http"

// Routes registers the project endpoints. Protected routes are wrapped with authed;
// POST /v1/projects is public by design (it is how a key is obtained).
func Routes(mux *http.ServeMux, h *Handler, authed func(http.Handler) http.Handler) {
	mux.Handle("POST /v1/projects", http.HandlerFunc(h.Create))
	mux.Handle("GET /v1/project", authed(http.HandlerFunc(h.GetCurrent)))
}

// MeRoutes registers the endpoints of the signed-in dashboard user, which accept a session only.
func MeRoutes(mux *http.ServeMux, h *MeHandler, session func(http.Handler) http.Handler) {
	mux.Handle("POST /v1/me/project", session(http.HandlerFunc(h.EnsureMine)))
}
