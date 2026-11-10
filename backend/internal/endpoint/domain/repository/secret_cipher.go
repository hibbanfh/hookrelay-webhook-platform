package repository

// SecretCipher seals and opens signing secrets for storage. The AAD binds a ciphertext to its
// row, and the key version says which key sealed it (ADR 0004). It uses only standard types so
// a library can satisfy it without importing the domain.
type SecretCipher interface {
	Seal(plaintext, aad []byte) (ciphertext []byte, keyVersion int, err error)
	Open(ciphertext, aad []byte, keyVersion int) ([]byte, error)
}
