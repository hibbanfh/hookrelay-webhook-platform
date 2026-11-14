package entity

import "time"

// DeliveryIDPrefix starts every delivery ID.
const DeliveryIDPrefix = "dlv_"

// PendingDelivery is the initial state of a delivery: one message for one endpoint, due
// immediately. The dispatch module owns the full delivery and its state machine (ADR 0013); this
// module only appends the first row.
type PendingDelivery struct {
	ID         string
	ProjectID  string
	MessageID  string
	EndpointID string
	CreatedAt  time.Time // also the time the first attempt is due
}

// NewPendingDelivery queues m for the endpoint, due at the message time.
func NewPendingDelivery(id string, m Message, endpointID string) PendingDelivery {
	return PendingDelivery{ID: id, ProjectID: m.ProjectID, MessageID: m.ID, EndpointID: endpointID, CreatedAt: m.CreatedAt}
}
