package httpx_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hookrelay/internal/platform/httpx"
)

type decodeBody struct {
	Name string `json:"name"`
}

func TestDecodeJSONAcceptsOneObject(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"a"}`))
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 1024)
	var dst decodeBody

	err := httpx.DecodeJSON(req, &dst)

	if err != nil || dst.Name != "a" {
		t.Fatalf("got %v %+v", err, dst)
	}
}

func TestDecodeJSONRejects(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		limit    int64
		wantCode int
	}{
		{name: "empty body", body: ``, limit: 1024, wantCode: 400},
		{name: "not json", body: `nope`, limit: 1024, wantCode: 400},
		{name: "unknown field", body: `{"name":"a","x":1}`, limit: 1024, wantCode: 400},
		{name: "wrong type stays generic", body: `{"name":5}`, limit: 1024, wantCode: 400},
		{name: "trailing value", body: `{"name":"a"}{"name":"b"}`, limit: 1024, wantCode: 400},
		{name: "too large", body: `{"name":"` + strings.Repeat("a", 100) + `"}`, limit: 10, wantCode: 413},
		{name: "oversize trailing data", body: `{"name":"a"}` + strings.Repeat(" ", 100) + `x`, limit: 20, wantCode: 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Body = http.MaxBytesReader(rec, req.Body, tt.limit)

			err := httpx.DecodeJSON(req, &decodeBody{})
			httpx.NewResponder(slog.New(slog.NewJSONHandler(io.Discard, nil))).Error(rec, req, err)

			if rec.Code != tt.wantCode {
				t.Fatalf("got status %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestDecodeJSONNamingFields(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{name: "wrong type names the field", body: `{"name":5}`, wantField: "name"},
		{name: "syntax errors stay generic", body: `nope`, wantField: ""},
		{name: "unknown fields stay generic", body: `{"x":1}`, wantField: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))

			err := httpx.DecodeJSONNamingFields(req, &decodeBody{})

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) || apiErr.Field != tt.wantField || apiErr.Status != 400 {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestDecodeJSONBytes(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		naming    bool
		wantOK    bool
		wantField string
	}{
		{name: "one object", body: `{"name":"a"}`, wantOK: true},
		{name: "empty", body: ``},
		{name: "unknown field", body: `{"name":"a","x":1}`},
		{name: "trailing value", body: `{"name":"a"}{"name":"b"}`},
		{name: "trailing garbage", body: `{"name":"a"} x`},
		{name: "wrong type stays generic", body: `{"name":5}`},
		{name: "wrong type names the field", body: `{"name":5}`, naming: true, wantField: "name"},
		{name: "syntax error stays generic when naming", body: `nope`, naming: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dst decodeBody
			decode := httpx.DecodeJSONBytes
			if tt.naming {
				decode = httpx.DecodeJSONBytesNamingFields
			}

			err := decode([]byte(tt.body), &dst)

			var apiErr *httpx.Error
			if tt.wantOK {
				if err != nil || dst.Name != "a" {
					t.Fatalf("got %v %+v", err, dst)
				}
				return
			}
			if !errors.As(err, &apiErr) || apiErr.Status != 400 || apiErr.Field != tt.wantField {
				t.Fatalf("got %v", err)
			}
		})
	}
}
