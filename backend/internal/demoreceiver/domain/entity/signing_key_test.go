package entity_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"hookrelay/internal/demoreceiver/domain/entity"
)

func TestSigningKeyRedactsItself(t *testing.T) {
	raw := []byte("super-secret-key-material-0123456789")
	key := entity.NewSigningKey(raw)
	asJSON, err := json.Marshal(struct{ Key entity.SigningKey }{key})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		out  string
	}{
		{name: "%v", out: fmt.Sprintf("%v", key)},
		{name: "%+v", out: fmt.Sprintf("%+v", key)},
		{name: "%#v", out: fmt.Sprintf("%#v", key)},
		{name: "%s", out: fmt.Sprintf("%s", key)},
		{name: "%x", out: fmt.Sprintf("%x", key)},
		{name: "inside a struct", out: fmt.Sprintf("%+v", entity.Candidate{EndpointID: "ep_1", Key: key})},
		{name: "json", out: string(asJSON)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.out, string(raw)) || !strings.Contains(tt.out, "whsec_****") {
				t.Fatalf("output %q must be redacted", tt.out)
			}
		})
	}
}

func TestSigningKeyCopiesItsBytes(t *testing.T) {
	raw := []byte("abc")
	key := entity.NewSigningKey(raw)
	raw[0] = 'X'
	got := key.Bytes()
	got[1] = 'Y'

	if string(key.Bytes()) != "abc" {
		t.Fatalf("key changed to %q", key.Bytes())
	}
}
