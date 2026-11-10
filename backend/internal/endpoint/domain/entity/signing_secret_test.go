package entity_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"hookrelay/internal/endpoint/domain/entity"
)

var secretPattern = regexp.MustCompile(`^whsec_[A-Za-z0-9+/]{43}=$`)

func newSecret(t *testing.T, fill byte) entity.SigningSecret {
	t.Helper()
	s, err := entity.NewSigningSecret(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSigningSecretFormat(t *testing.T) {
	secret := newSecret(t, 7)

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret.Value(), "whsec_"))

	if !secretPattern.MatchString(secret.Value()) || err != nil || len(decoded) < 24 || !bytes.Equal(decoded, secret.Bytes()) {
		t.Fatalf("value %q, decoded %d bytes, err %v", secret.Value(), len(decoded), err)
	}
}

func TestNewSigningSecretRequiresAtLeast24Bytes(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"23 bytes", 23, true},
		{"24 bytes", 24, false},
		{"32 bytes", 32, false},
		{"empty", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entity.NewSigningSecret(make([]byte, tt.size))

			if (err != nil) != tt.wantErr {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestDifferentEntropyGivesDifferentSecrets(t *testing.T) {
	if newSecret(t, 1).Value() == newSecret(t, 2).Value() {
		t.Fatal("secrets must differ")
	}
}

func TestSigningSecretIsRedactedEverywhereButValue(t *testing.T) {
	secret := newSecret(t, 9)
	plain := secret.Value()
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("created", slog.Any("secret", secret))
	slog.New(slog.NewTextHandler(&logs, nil)).Info("created", slog.Any("secret", secret))
	asJSON, err := json.Marshal(struct{ S entity.SigningSecret }{secret})
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{
		fmt.Sprint(secret), fmt.Sprintf("%v", secret), fmt.Sprintf("%+v", secret), fmt.Sprintf("%#v", secret),
		fmt.Sprintf("%s", secret), fmt.Sprintf("%q", secret), fmt.Sprintf("%x", secret), fmt.Sprintf("%d", secret),
		fmt.Sprintf("%v", &secret), fmt.Sprintf("%v", []entity.SigningSecret{secret}),
		fmt.Sprintf("%+v", struct{ S entity.SigningSecret }{secret}),
		logs.String(), string(asJSON),
	}

	for _, out := range outputs {
		if strings.Contains(out, plain) || strings.Contains(out, strings.TrimPrefix(plain, "whsec_")) {
			t.Fatalf("secret leaked in %q", out)
		}
	}
}
