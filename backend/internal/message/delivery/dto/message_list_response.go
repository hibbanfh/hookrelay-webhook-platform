package dto

import "time"

// MessageSummaryResponse is one item of the message list: no payload.
type MessageSummaryResponse struct {
	ID              string                 `json:"id"`
	EventType       string                 `json:"event_type"`
	CreatedAt       time.Time              `json:"created_at"`
	DeliverySummary DeliveryCountsResponse `json:"delivery_summary"`
}

// MessageListResponse is one page of messages. NextCursor is null on the last page.
type MessageListResponse struct {
	Data       []MessageSummaryResponse `json:"data"`
	NextCursor *string                  `json:"next_cursor"`
}
