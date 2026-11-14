package entity

// DeliveryTarget is what the delivery worker needs to call an endpoint: where, whether it is
// enabled, and the key to sign with. The secret redacts itself in every output.
type DeliveryTarget struct {
	EndpointID string
	ProjectID  string
	URL        string
	Enabled    bool
	Secret     SigningSecret
}
