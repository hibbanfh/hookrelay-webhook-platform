package repository

import "context"

// MessageCatalog reads the payloads of claimed deliveries. dispatch does not own the messages
// table: an adapter in the composition root calls the message module.
type MessageCatalog interface {
	// GetPayloads returns the stored payload bytes keyed by message ID. Unknown IDs are absent.
	GetPayloads(ctx context.Context, messageIDs []string) (map[string][]byte, error)
}
