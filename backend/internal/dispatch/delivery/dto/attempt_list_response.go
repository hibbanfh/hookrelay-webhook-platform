package dto

// AttemptListResponse is one page of attempts. NextCursor is null on the last page.
type AttemptListResponse struct {
	Data       []AttemptResponse `json:"data"`
	NextCursor *string           `json:"next_cursor"`
}
