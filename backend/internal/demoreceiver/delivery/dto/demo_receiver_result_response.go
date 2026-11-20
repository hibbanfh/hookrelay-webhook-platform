// Package dto holds the request and response structs of the demoreceiver HTTP API.
package dto

// DemoReceiverResultResponse is the 200 body of POST /demo/receiver/{project_id}
// (api/openapi.yaml schema DemoReceiverResult). Hookrelay stores it as the attempt's response body.
type DemoReceiverResultResponse struct {
	Received      bool   `json:"received"`
	EndpointID    string `json:"endpoint_id"`
	WebhookID     string `json:"webhook_id"`
	RequestNumber int    `json:"request_number"`
	FailFirst     int    `json:"fail_first"`
}
