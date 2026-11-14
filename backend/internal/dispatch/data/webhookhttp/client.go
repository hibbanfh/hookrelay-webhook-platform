// Package webhookhttp is the outbound adapter of the dispatch module: it performs the signed
// POST to a customer endpoint and turns every result into an entity.Outcome.
package webhookhttp

import (
	"crypto/tls"
	"net/http"
	"time"

	"hookrelay/internal/library/netguard"
)

const (
	// maxResponseHeaderBytes caps what a hostile receiver can make us buffer before the body.
	maxResponseHeaderBytes = 16 << 10
	idleConnTimeout        = 90 * time.Second
)

// NewClient builds an HTTP client whose every connection goes through dial (the SSRF guard).
// It never follows a redirect, never reads proxy variables from the environment and speaks TLS
// 1.2 or newer. It has no Timeout of its own: the per-attempt context is the one timeout.
func NewClient(dial netguard.DialFunc, maxIdlePerHost int) *http.Client {
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            dial,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		MaxResponseHeaderBytes: maxResponseHeaderBytes,
		MaxIdleConnsPerHost:    maxIdlePerHost,
		IdleConnTimeout:        idleConnTimeout,
		ForceAttemptHTTP2:      true,
	}
	return &http.Client{
		Transport: transport,
		// A 3xx is a failed attempt with its status, not a request to follow (ADR 0003).
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
