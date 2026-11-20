package httppresenter

import "net/http"

// Routes registers the demo receiver route. It has no guard: the webhook signature is the
// credential. The composition root calls it only when DEMO_RECEIVER_ENABLED is true. Any method
// other than POST is answered 405 by the mux.
func Routes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /demo/receiver/{project_id}", h.ReceiveWebhook)
}
