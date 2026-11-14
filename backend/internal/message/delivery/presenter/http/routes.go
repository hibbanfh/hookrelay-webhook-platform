package httppresenter

import "net/http"

// Routes registers the message routes. Every route is wrapped with authed, which accepts an API
// key or a dashboard session and puts the project ID in the context. Only POST exists until the
// list route arrives, so GET /v1/messages answers 405 with Allow: POST.
func Routes(mux *http.ServeMux, h *Handler, authed func(http.Handler) http.Handler) {
	mux.Handle("POST /v1/messages", authed(http.HandlerFunc(h.Publish)))
}
