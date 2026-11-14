package entity_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"hookrelay/internal/dispatch/domain/entity"
)

func TestSigningKeyRedactsItselfInEveryFormat(t *testing.T) {
	raw := []byte("super-secret-key-material-0123456789")
	key := entity.NewSigningKey(raw)
	asJSON, err := json.Marshal(struct{ Key entity.SigningKey }{key})
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", key),
		"%+v":  fmt.Sprintf("%+v", key),
		"%#v":  fmt.Sprintf("%#v", key),
		"%s":   fmt.Sprintf("%s", key), //nolint:gosimple // the verb is what is tested
		"json": string(asJSON),
	}
	for verb, out := range outputs {
		t.Run(verb, func(t *testing.T) {
			if strings.Contains(out, "secret") || !strings.Contains(out, "****") {
				t.Fatalf("output leaks or is not redacted: %q", out)
			}
		})
	}
}

func TestSigningKeyBytesReturnsACopy(t *testing.T) {
	key := entity.NewSigningKey([]byte{1, 2, 3})

	key.Bytes()[0] = 9

	if key.Bytes()[0] != 1 {
		t.Fatal("changing the returned slice must not change the key")
	}
}

func TestSigningKeyCopiesItsInput(t *testing.T) {
	raw := []byte{1, 2, 3}
	key := entity.NewSigningKey(raw)

	raw[0] = 9

	if key.Bytes()[0] != 1 {
		t.Fatal("the key must not alias the caller's slice")
	}
}
