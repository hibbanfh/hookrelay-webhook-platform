package model

// SealedTarget is an endpoints row as the delivery worker reads it: the URL and flag plus the
// sealed secret columns.
type SealedTarget struct {
	ID         string
	ProjectID  string
	URL        string
	Enabled    bool
	Ciphertext []byte
	KeyVersion int
}
