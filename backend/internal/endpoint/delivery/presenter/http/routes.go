package httppresenter

import "net/http"

// Routes registers the endpoint routes. Every route is wrapped with authed, which accepts an API
// key or a dashboard session and puts the project ID in the context.
func Routes(mux *http.ServeMux, h *Handler, authed func(http.Handler) http.Handler) {
	mux.Handle("POST /v1/endpoints", authed(http.HandlerFunc(h.Create)))
	mux.Handle("GET /v1/endpoints", authed(http.HandlerFunc(h.List)))
	mux.Handle("GET /v1/endpoints/{endpoint_id}", authed(http.HandlerFunc(h.Get)))
	mux.Handle("PATCH /v1/endpoints/{endpoint_id}", authed(http.HandlerFunc(h.Update)))
	mux.Handle("GET /v1/endpoints/{endpoint_id}/secret", authed(http.HandlerFunc(h.GetSecret)))
}
