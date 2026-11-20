package entity

// EndpointSecret pairs an endpoint with its signing secret. It is what the internal demo secret
// listing returns; no HTTP route exposes it.
type EndpointSecret struct {
	EndpointID string
	Secret     SigningSecret
}
