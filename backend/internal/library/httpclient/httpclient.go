// Package httpclient builds the outbound HTTP client used to call trusted services.
package httpclient

import (
	"net/http"
	"time"

	"github.com/google/wire"
)

// ProviderSet provides the outbound client.
var ProviderSet = wire.NewSet(New)

// Options configure the client.
type Options struct {
	Timeout time.Duration
}

// New returns a client with a timeout that does not follow redirects: a redirect from the
// service is an error to the caller, never a second request to somewhere else.
func New(o Options) *http.Client {
	return &http.Client{
		Timeout:       o.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
