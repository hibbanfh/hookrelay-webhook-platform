package entity

// AttemptError names why an attempt got no HTTP response.
type AttemptError string

// Attempt errors. They match the CHECK constraint of the attempts table.
const (
	ErrorNone               AttemptError = ""
	ErrorTimeout            AttemptError = "timeout"
	ErrorConnection         AttemptError = "connection_error"
	ErrorDNS                AttemptError = "dns_error"
	ErrorDestinationBlocked AttemptError = "destination_blocked"
	ErrorEndpointDisabled   AttemptError = "endpoint_disabled"
)

// IsTerminal reports errors that a retry cannot fix: the destination is forbidden or the
// endpoint is switched off.
func (e AttemptError) IsTerminal() bool {
	return e == ErrorDestinationBlocked || e == ErrorEndpointDisabled
}
