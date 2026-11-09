package httpx_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hookrelay/internal/platform/httpx"
)

func TestDecodeJSON(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name     string
		body     string
		limit    int64
		wantName string
		wantCode int
	}{
		{name: "valid", body: `{"name":"a"}`, limit: 1024, wantName: "a"},
		{name: "empty body", body: ``, limit: 1024, wantCode: 400},
		{name: "not json", body: `nope`, limit: 1024, wantCode: 400},
		{name: "unknown field", body: `{"name":"a","x":1}`, limit: 1024, wantCode: 400},
		{name: "wrong type", body: `{"name":5}`, limit: 1024, wantCode: 400},
		{name: "trailing value", body: `{"name":"a"}{"name":"b"}`, limit: 1024, wantCode: 400},
		{name: "too large", body: `{"name":"` + strings.Repeat("a", 100) + `"}`, limit: 10, wantCode: 413},
		{name: "oversize trailing data", body: `{"name":"a"}` + strings.Repeat(" ", 100) + `x`, limit: 20, wantCode: 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Body = http.MaxBytesReader(rec, req.Body, tt.limit)
			var dst body
			err := httpx.DecodeJSON(req, &dst)
			if tt.wantCode == 0 {
				if err != nil || dst.Name != tt.wantName {
					t.Fatalf("got %v %+v", err, dst)
				}
				return
			}
			httpx.NewResponder(slog.New(slog.NewJSONHandler(io.Discard, nil))).Error(rec, req, err)
			if rec.Code != tt.wantCode {
				t.Fatalf("got status %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}
