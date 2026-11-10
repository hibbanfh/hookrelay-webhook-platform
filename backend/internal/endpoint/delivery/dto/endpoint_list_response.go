package dto

// EndpointListResponse is one page of endpoints. NextCursor is null on the last page.
type EndpointListResponse struct {
	Data       []EndpointResponse `json:"data"`
	NextCursor *string            `json:"next_cursor"`
}
