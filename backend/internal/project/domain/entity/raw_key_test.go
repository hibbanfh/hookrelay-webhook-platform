package entity_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

func TestNewRawKey(t *testing.T) {
	tests := []struct {
		name    string
		entropy []byte
		wantErr bool
	}{
		{"32 bytes", bytes.Repeat([]byte{7}, 32), false},
		{"too short", bytes.Repeat([]byte{7}, 31), true},
		{"too long", bytes.Repeat([]byte{7}, 33), true},
		{"nil", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := entity.NewRawKey(tt.entropy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err %v", err)
			}
			if err != nil {
				return
			}
			if _, perr := entity.ParseRawKey(k.Reveal()); perr != nil {
				t.Fatalf("generated key must parse: %v", perr)
			}
			if len(k.Reveal()) != 46 || !strings.HasPrefix(k.Reveal(), "hr_") {
				t.Fatalf("bad format %q", k.Reveal())
			}
		})
	}
}

func TestParseRawKey(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid", "hr_" + strings.Repeat("a", 43), false},
		{"valid with url-safe chars", "hr_" + strings.Repeat("-_Az09", 7) + "a", false},
		{"empty", "", true},
		{"wrong prefix", "xx_" + strings.Repeat("a", 43), true},
		{"no prefix", strings.Repeat("a", 46), true},
		{"too short", "hr_" + strings.Repeat("a", 42), true},
		{"too long", "hr_" + strings.Repeat("a", 44), true},
		{"bad char", "hr_" + strings.Repeat("a", 42) + "+", true},
		{"padding char", "hr_" + strings.Repeat("a", 42) + "=", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entity.ParseRawKey(tt.in)
			if tt.wantErr && !errors.Is(err, domainerrors.ErrMalformedKey) {
				t.Fatalf("want ErrMalformedKey, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected %v", err)
			}
		})
	}
}

func TestRawKeyNeverPrintsItsSecret(t *testing.T) {
	k, err := entity.ParseRawKey("hr_" + strings.Repeat("s", 43))
	if err != nil {
		t.Fatal(err)
	}
	holder := struct{ Key entity.RawKey }{k}
	outputs := map[string]string{
		"String":       k.String(),
		"%v %+v %s":    fmt.Sprintf("%v %+v %s", k, k, k),
		"%#v":          fmt.Sprintf("%#v", k),
		"%d %x %X %q":  fmt.Sprintf("%d %x %X %q", k, k, k, k),
		"%t %o %b":     fmt.Sprintf("%t %o %b", k, k, k),
		"pointer":      fmt.Sprintf("%v %+v %#v", &k, &k, &k),
		"Sprint":       fmt.Sprint(k, &k),
		"slice":        fmt.Sprintf("%v %#v", []entity.RawKey{k}, []entity.RawKey{k}),
		"struct field": fmt.Sprintf("%v %+v %#v", holder, holder, holder),
		"map value":    fmt.Sprintf("%v", map[string]entity.RawKey{"k": k}),
	}
	secrets := []string{"sss", "737373", "c3Nz"}
	for name, out := range outputs {
		for _, secret := range secrets {
			if strings.Contains(out, secret) {
				t.Fatalf("%s leaked the secret (%q): %q", name, secret, out)
			}
		}
	}
}
