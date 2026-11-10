package repository

import "context"

// URLPolicy decides whether a URL may be registered as a destination (SSRF protection). Any
// non-nil error means the URL is rejected, and its text is safe to show to the client.
type URLPolicy interface {
	Check(ctx context.Context, rawURL string) error
}
