package dto

// DeliveryCountsResponse is how many deliveries of a message are in each status.
type DeliveryCountsResponse struct {
	Pending   int `json:"pending"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}
