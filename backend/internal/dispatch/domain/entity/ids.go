package entity

import "regexp"

// DeliveryIDPrefix starts every delivery ID.
const DeliveryIDPrefix = "dlv_"

// The ID shapes the ID generator produces: the prefix and 26 Crockford base32 characters
// (api/openapi.yaml, schemas DeliveryID and EndpointID).
var (
	deliveryIDPattern = regexp.MustCompile(`^dlv_[0-9A-HJKMNP-TV-Z]{26}$`)
	endpointIDPattern = regexp.MustCompile(`^ep_[0-9A-HJKMNP-TV-Z]{26}$`)
)

// IsDeliveryID reports whether s is a well-formed delivery ID. Delivery answers 404 for anything
// else, so a malformed ID never reaches the database.
func IsDeliveryID(s string) bool { return deliveryIDPattern.MatchString(s) }

// IsEndpointID reports whether s is a well-formed endpoint ID. dispatch cannot import the
// endpoint module, so it keeps its own copy of the shape.
func IsEndpointID(s string) bool { return endpointIDPattern.MatchString(s) }
