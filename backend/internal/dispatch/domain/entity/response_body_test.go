package entity_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"hookrelay/internal/dispatch/domain/entity"
)

func TestNewResponseBody(t *testing.T) {
	tests := []struct {
		name          string
		raw           []byte
		wantTruncated bool
		wantText      string // checked only when set
	}{
		{name: "empty", raw: nil},
		{name: "short", raw: []byte("ok"), wantText: "ok"},
		{name: "exactly 4096 ASCII is not truncated", raw: bytes.Repeat([]byte("a"), 4096)},
		{name: "4097 bytes is truncated", raw: bytes.Repeat([]byte("a"), 4097), wantTruncated: true},
		{name: "1 MB is truncated", raw: bytes.Repeat([]byte("a"), 1<<20), wantTruncated: true},
		{
			name:          "a 3-byte rune split at byte 4096",
			raw:           append(bytes.Repeat([]byte("a"), 4095), []byte("€")...),
			wantTruncated: true,
			wantText:      strings.Repeat("a", 4095),
		},
		{
			name:          "invalid UTF-8 that expands past 4096",
			raw:           bytes.Repeat([]byte{0xff, 'a'}, 3000),
			wantTruncated: true,
		},
		{name: "NUL bytes are removed", raw: []byte("a\x00b\x00"), wantText: "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entity.NewResponseBody(tt.raw)

			if len(got.Text) > entity.MaxResponseBodyBytes || !utf8.ValidString(got.Text) || strings.ContainsRune(got.Text, 0) {
				t.Fatalf("stored text is not safe for a text column: %d bytes, valid %v", len(got.Text), utf8.ValidString(got.Text))
			}
			if got.Truncated != tt.wantTruncated {
				t.Fatalf("truncated = %v, want %v", got.Truncated, tt.wantTruncated)
			}
			if tt.wantText != "" && got.Text != tt.wantText {
				t.Fatalf("text = %q, want %q", got.Text, tt.wantText)
			}
		})
	}
}
