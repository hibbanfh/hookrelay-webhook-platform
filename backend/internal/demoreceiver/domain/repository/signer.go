package repository

// Signer computes a Standard Webhooks signature. It is implemented in backend/wire over the
// function the delivery worker signs with, so the HMAC code exists once (ADR 0017 section 2).
type Signer interface {
	// Sign returns "v1," + base64(HMAC-SHA256(key, "{webhookID}.{ts}.{body}")).
	Sign(key []byte, webhookID string, ts int64, body []byte) string
}
