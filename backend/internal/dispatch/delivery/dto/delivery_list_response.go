package dto

// DeliveryListResponse is one page of deliveries. NextCursor is null on the last page.
type DeliveryListResponse struct {
	Data       []DeliveryResponse `json:"data"`
	NextCursor *string            `json:"next_cursor"`
}
