package main

import (
	"net/http"
	"testing"
	"time"
)

// Values from docs/architecture.md section 12 (DoS row).
func TestNewServerTimeouts(t *testing.T) {
	srv := newServer(":0", http.NotFoundHandler())

	got := [4]time.Duration{srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout}
	want := [4]time.Duration{5 * time.Second, 15 * time.Second, 20 * time.Second, 120 * time.Second}
	if got != want {
		t.Fatalf("header/read/write/idle timeouts = %v, want %v", got, want)
	}
}

func TestServerWriteTimeoutOutlastsTheRequestTimeout(t *testing.T) {
	if writeTimeout <= requestTimeout {
		t.Fatalf("WriteTimeout %v must exceed the per-request timeout %v or clients see resets instead of JSON errors", writeTimeout, requestTimeout)
	}
}
