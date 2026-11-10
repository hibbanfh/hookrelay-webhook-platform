package httpx_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/platform/httpx"
)

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 11, 9, 12, 30, 45, 123456000, time.UTC)

	gotAt, gotID, err := httpx.DecodeCursor(httpx.EncodeCursor(at, "ep_01ABC"))

	if err != nil || !gotAt.Equal(at) || gotID != "ep_01ABC" {
		t.Fatalf("got %v %q %v", gotAt, gotID, err)
	}
}

func TestDecodeCursorRejectsMalformedValuesOnTheCursorField(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
	}{
		{"not base64", "!!!"},
		{"wrong version", "djJ8MjAyNi0xMS0wOVQxMjowMDowMFp8ZXBfMQ"},
		{"no separators", "aGVsbG8"},
		{"empty", ""},
		{"bad timestamp", "djF8bm90LWEtdGltZXxlcF8x"},
		{"empty id", "djF8MjAyNi0xMS0wOVQxMjowMDowMFp8"},
		{"over-long id", "djF8MjAyNi0xMS0wOVQxMjowMDowMFp8YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFh"},
		{"too long", strings.Repeat("a", 513)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := httpx.DecodeCursor(tt.cursor)

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) || apiErr.Field != "cursor" || apiErr.Status != 400 {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{"absent uses the default", "", 20, false},
		{"minimum", "1", 1, false},
		{"maximum", "100", 100, false},
		{"zero", "0", 0, true},
		{"above the maximum", "101", 0, true},
		{"negative", "-1", 0, true},
		{"not a number", "abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpx.ParseLimit(tt.raw)

			var apiErr *httpx.Error
			isFieldError := errors.As(err, &apiErr) && apiErr.Field == "limit"
			if got != tt.want || isFieldError != tt.wantErr {
				t.Fatalf("got %d, %v", got, err)
			}
		})
	}
}
