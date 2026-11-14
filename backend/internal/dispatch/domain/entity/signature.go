package entity

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
)

// SignatureVersion prefixes every signature (Standard Webhooks, ADR 0007).
const SignatureVersion = "v1"

// Sign returns "v1," + base64(HMAC-SHA256(key, "{msgID}.{ts}.{body}")). The body stays a byte
// slice throughout: a string round trip must never touch the bytes the receiver verifies.
func Sign(key []byte, msgID string, ts int64, body []byte) string {
	var content bytes.Buffer
	content.WriteString(msgID)
	content.WriteByte('.')
	content.WriteString(strconv.FormatInt(ts, 10))
	content.WriteByte('.')
	content.Write(body)
	mac := hmac.New(sha256.New, key)
	mac.Write(content.Bytes())
	return SignatureVersion + "," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
