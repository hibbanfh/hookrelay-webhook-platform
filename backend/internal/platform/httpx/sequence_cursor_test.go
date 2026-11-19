package httpx_test

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/platform/httpx"
)

func TestSequenceCursorRoundTrip(t *testing.T) {
	got, err := httpx.DecodeSequenceCursor(httpx.EncodeSequenceCursor(42))

	if err != nil || got != 42 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestDecodeSequenceCursorRejectsEverythingElse(t *testing.T) {
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	tests := []struct {
		name   string
		cursor string
	}{
		{"empty", ""},
		{"not base64", "!!!"},
		{"wrong tag", b64("s2|5")},
		{"keyset cursor", httpx.EncodeCursor(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), "dlv_1")},
		{"not an integer", b64("s1|abc")},
		{"zero", b64("s1|0")},
		{"over int4", b64("s1|2147483648")},
		{"far over int4", b64("s1|3000000000")},
		{"plus sign", b64("s1|+1")},
		{"leading zero", b64("s1|01")},
		{"negative", b64("s1|-3")},
		{"no number", b64("s1|")},
		{"extra part", b64("s1|1|2")},
		{"too long", strings.Repeat("a", 513)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := httpx.DecodeSequenceCursor(tt.cursor)

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || apiErr.Field != "cursor" {
				t.Fatalf("got %v, want 400 on field cursor", err)
			}
		})
	}
}

func TestDecodeCursorRejectsASequenceCursor(t *testing.T) {
	_, _, err := httpx.DecodeCursor(httpx.EncodeSequenceCursor(3))

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) || apiErr.Field != "cursor" {
		t.Fatalf("got %v, want 400 on field cursor", err)
	}
}

func TestOptionalQueryValue(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		wantValue   string
		wantPresent bool
		wantErr     bool
	}{
		{name: "absent", query: ""},
		{name: "present", query: "status=failed", wantValue: "failed", wantPresent: true},
		{name: "present but empty", query: "status=", wantPresent: true},
		{name: "repeated", query: "status=failed&status=pending", wantErr: true},
		{name: "repeated empty", query: "status=&status=", wantErr: true},
		{name: "other parameter", query: "limit=5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x?"+tt.query, nil)

			value, present, err := httpx.OptionalQueryValue(r, "status")

			var apiErr *httpx.Error
			if tt.wantErr {
				if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || apiErr.Field != "status" {
					t.Fatalf("got %v, want 400 on field status", err)
				}
				return
			}
			if err != nil || value != tt.wantValue || present != tt.wantPresent {
				t.Fatalf("got %q, %v, %v", value, present, err)
			}
		})
	}
}

func TestWriteRawJSONWritesTheBytesUnchanged(t *testing.T) {
	body := []byte("{\"payload\":{\"b\":1,  \"a\":\"<&>\", \"e\":\"\u00e9\"}}")
	rec := httptest.NewRecorder()

	httpx.WriteRawJSON(rec, http.StatusOK, body)

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != string(body) {
		t.Fatalf("body %q, want %q", rec.Body.String(), body)
	}
}

func TestDecodeSequenceCursorAcceptsTheLargestInt4(t *testing.T) {
	got, err := httpx.DecodeSequenceCursor(base64.RawURLEncoding.EncodeToString([]byte("s1|2147483647")))

	if err != nil || got != 2147483647 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestListParametersSentTwiceAreRejected(t *testing.T) {
	tests := []struct {
		name  string
		query string
		field string
	}{
		{"limit twice", "limit=1&limit=2", "limit"},
		{"cursor twice", "cursor=a&cursor=b", "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x?"+tt.query, nil)

			_, limitErr := httpx.LimitFromQuery(r)
			_, cursorErr := httpx.CursorFromQuery(r)

			var apiErr *httpx.Error
			err := limitErr
			if tt.field == "cursor" {
				err = cursorErr
			}
			if !errors.As(err, &apiErr) || apiErr.Field != tt.field {
				t.Fatalf("got %v, want 400 on %s", err, tt.field)
			}
		})
	}
}

func TestLimitAndCursorFromQueryDefaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x?limit=7&cursor=abc", nil)
	limit, err1 := httpx.LimitFromQuery(r)
	cursor, err2 := httpx.CursorFromQuery(r)
	empty, err3 := httpx.LimitFromQuery(httptest.NewRequest(http.MethodGet, "/x", nil))

	if err1 != nil || err2 != nil || err3 != nil || limit != 7 || cursor != "abc" || empty != httpx.DefaultLimit {
		t.Fatalf("got %d %q %d, errors %v %v %v", limit, cursor, empty, err1, err2, err3)
	}
}
