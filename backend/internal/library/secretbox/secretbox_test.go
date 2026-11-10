package secretbox_test

import (
	"bytes"
	"errors"
	"testing"

	"hookrelay/internal/library/secretbox"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func newBox(t *testing.T, o secretbox.Options) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(o)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func TestSealOpenRoundTrip(t *testing.T) {
	box := newBox(t, secretbox.Options{Key: key(1)})
	plaintext := []byte("a signing secret")

	sealed, version, err := box.Seal(plaintext, []byte("ep_1"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := box.Open(sealed, []byte("ep_1"), version)

	if err != nil || !bytes.Equal(opened, plaintext) || bytes.Contains(sealed, plaintext) {
		t.Fatalf("opened %q, err %v, plaintext visible in ciphertext: %v", opened, err, bytes.Contains(sealed, plaintext))
	}
}

func TestSealUsesAFreshNonceEveryTime(t *testing.T) {
	box := newBox(t, secretbox.Options{Key: key(1)})

	a, _, errA := box.Seal([]byte("same"), []byte("ep_1"))
	b, _, errB := box.Seal([]byte("same"), []byte("ep_1"))

	if errA != nil || errB != nil || bytes.Equal(a, b) {
		t.Fatalf("two seals of one plaintext must differ: %v %v", errA, errB)
	}
}

func TestOpenFailures(t *testing.T) {
	box := newBox(t, secretbox.Options{Key: key(1)})
	sealed, version, err := box.Seal([]byte("secret"), []byte("ep_1"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xFF
	tests := []struct {
		name       string
		ciphertext []byte
		aad        []byte
		version    int
		want       error
	}{
		{"another row's id as AAD", sealed, []byte("ep_2"), version, secretbox.ErrOpen},
		{"tampered ciphertext", tampered, []byte("ep_1"), version, secretbox.ErrOpen},
		{"too short to be a sealed box", []byte("short"), []byte("ep_1"), version, secretbox.ErrOpen},
		{"unknown key version", sealed, []byte("ep_1"), version + 1, secretbox.ErrUnknownKeyVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := box.Open(tt.ciphertext, tt.aad, tt.version)

			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRotationOpensRowsSealedWithThePreviousKey(t *testing.T) {
	old := newBox(t, secretbox.Options{Key: key(1)})
	sealed, oldVersion, err := old.Seal([]byte("secret"), []byte("ep_1"))
	if err != nil {
		t.Fatal(err)
	}
	rotated := newBox(t, secretbox.Options{Key: key(2), PreviousKey: key(1)})

	opened, openErr := rotated.Open(sealed, []byte("ep_1"), oldVersion)
	_, newVersion, sealErr := rotated.Seal([]byte("secret"), []byte("ep_1"))

	if openErr != nil || sealErr != nil || string(opened) != "secret" || newVersion == oldVersion {
		t.Fatalf("open %v, seal %v, versions %d -> %d", openErr, sealErr, oldVersion, newVersion)
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	tests := []struct {
		name string
		opts secretbox.Options
	}{
		{"missing key", secretbox.Options{}},
		{"short key", secretbox.Options{Key: []byte("short")}},
		{"short previous key", secretbox.Options{Key: key(1), PreviousKey: []byte("short")}},
		{"previous equals current (same derived version)", secretbox.Options{Key: key(1), PreviousKey: key(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := secretbox.New(tt.opts)

			if err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestVersionIsDerivedFromTheKeyAndStaysInSmallintRange(t *testing.T) {
	a := newBox(t, secretbox.Options{Key: key(1)})
	b := newBox(t, secretbox.Options{Key: key(1)})

	_, versionA, errA := a.Seal([]byte("x"), nil)
	_, versionB, errB := b.Seal([]byte("x"), nil)

	if errA != nil || errB != nil || versionA != versionB || versionA < 1 || versionA > 32767 {
		t.Fatalf("versions %d %d (%v %v)", versionA, versionB, errA, errB)
	}
}
