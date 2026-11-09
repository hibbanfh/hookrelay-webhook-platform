package health_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hookrelay/internal/platform/health"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestHandler(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		want     int
		wantBody string
	}{
		{"healthy", nil, http.StatusOK, `{"status":"ok","checks":{"database":"ok"}}`},
		{"db down", errors.New("down"), http.StatusServiceUnavailable, `{"status":"unavailable","checks":{"database":"unreachable"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			health.Handler(fakePinger{err: tt.err})(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != tt.want {
				t.Fatalf("got %d, want %d", rec.Code, tt.want)
			}
			if strings.TrimSpace(rec.Body.String()) != tt.wantBody {
				t.Fatalf("body %s, want %s", rec.Body.String(), tt.wantBody)
			}
		})
	}
}
