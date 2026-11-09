package entity_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

func TestParseSessionTokenAccepts(t *testing.T) {
	tests := []struct{ name, in string }{
		{"neon style", "abc.DEF_123-xyz%2B%3D"},
		{"one char", "a"},
		{"2048 chars", strings.Repeat("a", 2048)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok, err := entity.ParseSessionToken(tt.in)

			if err != nil || tok.Value() != tt.in {
				t.Fatalf("got %q, %v", tok.Value(), err)
			}
		})
	}
}

func TestParseSessionTokenRejects(t *testing.T) {
	tests := []struct{ name, in string }{
		{"empty", ""},
		{"2049 chars", strings.Repeat("a", 2049)},
		{"space", "ab cd"},
		{"semicolon", "ab;cd"},
		{"comma", "ab,cd"},
		{"double quote", `ab"cd`},
		{"backslash", `ab\cd`},
		{"control char", "ab\x01cd"},
		{"newline", "ab\ncd"},
		{"non ascii", "ab\u00e9"},
		{"DEL", "ab\x7fcd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entity.ParseSessionToken(tt.in)

			if !errors.Is(err, domainerrors.ErrUnauthorized) {
				t.Fatalf("got %v, want ErrUnauthorized", err)
			}
		})
	}
}

func TestSessionTokenIsRedactedInEveryFormat(t *testing.T) {
	const secret = "supersecrettokenvalue"
	tok, err := entity.ParseSessionToken(secret)
	if err != nil {
		t.Fatal(err)
	}

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
		out := fmt.Sprintf(verb, tok)
		if strings.Contains(out, secret) {
			t.Errorf("%s leaked the token: %s", verb, out)
		}
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("request", slog.Any("token", tok))
	if strings.Contains(logs.String(), secret) {
		t.Errorf("a structured log leaked the token: %s", logs.String())
	}
}
