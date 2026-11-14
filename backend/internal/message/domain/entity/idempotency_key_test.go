package entity_test

import (
	"errors"
	"strings"
	"testing"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
)

func TestParseIdempotencyKey(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		valid bool
	}{
		{name: "uuid", raw: "7f3c9b2e-1d4a-4e8b-9a61-2c0f5d8e7b14", valid: true},
		{name: "one character", raw: "a", valid: true},
		{name: "255 characters", raw: strings.Repeat("k", 255), valid: true},
		{name: "printable symbols", raw: "!~#$%&()*+,-./:;<=>?@[]^_{|}", valid: true},
		{name: "empty", raw: ""},
		{name: "256 characters", raw: strings.Repeat("k", 256)},
		{name: "leading space", raw: " a"},
		{name: "inner space", raw: "a b"},
		{name: "tab", raw: "a\tb"},
		{name: "trailing newline", raw: "abc\n"},
		{name: "DEL", raw: "a\x7f"},
		{name: "non-ASCII", raw: "clé"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := entity.ParseIdempotencyKey(tt.raw)

			if tt.valid {
				if err != nil || string(key) != tt.raw {
					t.Fatalf("got %q %v", key, err)
				}
				return
			}
			var invalid *domainerrors.ValidationError
			if !errors.As(err, &invalid) || invalid.Field != entity.IdempotencyKeyHeader {
				t.Fatalf("got %v, want a ValidationError on %s", err, entity.IdempotencyKeyHeader)
			}
		})
	}
}
