package entity

// Job is a claimed delivery with everything one attempt needs.
type Job struct {
	Delivery Delivery
	Target   Target
	Payload  []byte
}

// SkipReason says why a claimed delivery could not become a Job.
type SkipReason string

// Skip reasons.
const (
	SkipTargetMissing    SkipReason = "target_missing"
	SkipSecretUnreadable SkipReason = "secret_unreadable"
	SkipPayloadMissing   SkipReason = "payload_missing"
)

// Skipped is a claimed delivery left leased: it comes back when its lease expires.
type Skipped struct {
	DeliveryID string
	Reason     SkipReason
	Detail     string // a non-secret cause for the log, empty when the reason says it all
}
