package entity

import "time"

// HitRetention is how long a counter may sit idle before it starts again from 1. It also bounds
// the table: expired rows are swept away on the write path (ADR 0017 section 1).
const HitRetention = 7 * 24 * time.Hour

// HitKey identifies one counter: the demo endpoint and the webhook-id a request carried. The
// counter is per endpoint so that two flaky endpoints fed by one message each fail on their own.
type HitKey struct{ EndpointID, WebhookID string }
