package httpclient_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hookrelay/internal/library/httpclient"
)

func TestNewDoesNotFollowRedirectsAndKeepsTheTimeout(t *testing.T) {
	followed := false
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed = true }))
	defer other.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer redirecting.Close()
	client := httpclient.New(httpclient.Options{Timeout: 3 * time.Second})

	resp, err := client.Get(redirecting.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test response body

	if resp.StatusCode != http.StatusFound || followed || client.Timeout != 3*time.Second {
		t.Fatalf("status %d, redirect followed = %v, timeout %v", resp.StatusCode, followed, client.Timeout)
	}
}
