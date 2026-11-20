package model

// SealedSecret is the sealed signing secret of one endpoint, as the demo secret listing reads it.
type SealedSecret struct {
	EndpointID string
	Ciphertext []byte
	KeyVersion int
}
