// Package health provides the /healthz handler.
package health

import (
	"context"
	"net/http"
	"time"

	"hookrelay/internal/platform/httpx"
)

const pingTimeout = time.Second

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Handler reports 200 when the database answers within one second, otherwise 503.
// The body follows the Health schema in api/openapi.yaml.
func Handler(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, response{Status: "unavailable", Checks: map[string]string{"database": "unreachable"}})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response{Status: "ok", Checks: map[string]string{"database": "ok"}})
	}
}
