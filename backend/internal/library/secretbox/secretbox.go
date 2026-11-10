// Package secretbox seals small secrets with AES-256-GCM (ADR 0004). A sealed value is
// nonce || ciphertext || tag. The additional authenticated data binds a value to its row, so a
// ciphertext copied to another row fails to open.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/google/wire"
)

// ProviderSet provides the box.
var ProviderSet = wire.NewSet(New)

const (
	keyBytes = 32
	// maxVersion keeps the derived version inside a PostgreSQL smallint.
	maxVersion = 32767
)

// Errors returned by Open. They carry no key or plaintext material.
var (
	ErrOpen              = errors.New("secretbox: cannot open sealed value")
	ErrUnknownKeyVersion = errors.New("secretbox: unknown key version")
)

// Options configure the box. PreviousKey is optional and only used to open older values.
type Options struct {
	Key         []byte
	PreviousKey []byte
}

type keyring struct {
	aead    cipher.AEAD
	version int
}

// Box seals with the current key and opens with the current or previous key.
type Box struct {
	current  keyring
	previous *keyring
}

// New validates the keys. It fails when a key has the wrong size or when the current and
// previous keys derive the same version, which would make a stored value ambiguous.
func New(o Options) (*Box, error) {
	current, err := newKeyring(o.Key)
	if err != nil {
		return nil, fmt.Errorf("secretbox key: %w", err)
	}
	box := &Box{current: current}
	if len(o.PreviousKey) == 0 {
		return box, nil
	}
	previous, err := newKeyring(o.PreviousKey)
	if err != nil {
		return nil, fmt.Errorf("secretbox previous key: %w", err)
	}
	if previous.version == current.version {
		return nil, errors.New("secretbox: current and previous keys derive the same version")
	}
	box.previous = &previous
	return box, nil
}

func newKeyring(key []byte) (keyring, error) {
	if len(key) != keyBytes {
		return keyring{}, fmt.Errorf("must be %d bytes", keyBytes)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return keyring{}, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return keyring{}, fmt.Errorf("create gcm: %w", err)
	}
	return keyring{aead: aead, version: deriveVersion(key)}, nil
}

// deriveVersion is 1 + (first two SHA-256 bytes of the key) mod 32767, so operators keep no
// version bookkeeping. The version is a label for the key, not a secret.
func deriveVersion(key []byte) int {
	sum := sha256.Sum256(key)
	return 1 + int(binary.BigEndian.Uint16(sum[:2]))%maxVersion
}

// Seal encrypts plaintext with a fresh nonce and returns the sealed value and the key version.
func (b *Box) Seal(plaintext, aad []byte) (sealed []byte, version int, err error) {
	nonce := make([]byte, b.current.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, 0, fmt.Errorf("read nonce: %w", err)
	}
	return b.current.aead.Seal(nonce, nonce, plaintext, aad), b.current.version, nil
}

// Open decrypts a value sealed under the given key version.
func (b *Box) Open(sealed, aad []byte, version int) ([]byte, error) {
	ring, ok := b.keyring(version)
	if !ok {
		return nil, ErrUnknownKeyVersion
	}
	nonceSize := ring.aead.NonceSize()
	if len(sealed) < nonceSize+ring.aead.Overhead() {
		return nil, ErrOpen
	}
	plaintext, err := ring.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return plaintext, nil
}

func (b *Box) keyring(version int) (keyring, bool) {
	if version == b.current.version {
		return b.current, true
	}
	if b.previous != nil && version == b.previous.version {
		return *b.previous, true
	}
	return keyring{}, false
}
