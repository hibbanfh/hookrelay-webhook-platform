package httppresenter

import "net/http"

// Routes registers the dispatch routes. Every route is wrapped with authed, which accepts an API
// key or a dashboard session and puts the project ID in the context. The patterns do not conflict
// with the endpoint module: it owns the paths below /v1/endpoints/{endpoint_id} without /deliveries.
// /replay accepts POST only, so any other method on it is answered 405 by the mux.
func Routes(mux *http.ServeMux, h *Handler, authed func(http.Handler) http.Handler) {
	mux.Handle("GET /v1/endpoints/{endpoint_id}/deliveries", authed(http.HandlerFunc(h.ListEndpointDeliveries)))
	mux.Handle("GET /v1/deliveries/{delivery_id}/attempts", authed(http.HandlerFunc(h.ListDeliveryAttempts)))
	mux.Handle("POST /v1/deliveries/{delivery_id}/replay", authed(http.HandlerFunc(h.ReplayDelivery)))
}
