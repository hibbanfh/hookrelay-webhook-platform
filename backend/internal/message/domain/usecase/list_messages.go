package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/repository"
)

// ListMessages returns one page of the messages of a project with their delivery counts, newest
// first (GET /v1/messages).
type ListMessages struct {
	messages   repository.MessageRepository
	deliveries repository.DeliveryReader
}

// NewListMessages builds the use case.
func NewListMessages(m repository.MessageRepository, d repository.DeliveryReader) *ListMessages {
	return &ListMessages{messages: m, deliveries: d}
}

// ListMessagesInput selects the page. After is nil for the first page.
type ListMessagesInput struct {
	ProjectID string
	Limit     int
	After     *entity.PageAnchor
}

// MessageListItem is a message without its payload and the counts of its deliveries.
type MessageListItem struct {
	Header  entity.MessageHeader
	Summary entity.DeliverySummary
}

// ListMessagesOutput is the page and the anchor of the next one (nil on the last page).
type ListMessagesOutput struct {
	Items []MessageListItem
	Next  *entity.PageAnchor
}

// Execute asks for one row more than the limit; the extra row proves a next page exists. The
// counts of the whole page come from one call, so a page costs two queries whatever its size.
func (u *ListMessages) Execute(ctx context.Context, in ListMessagesInput) (ListMessagesOutput, error) {
	if in.Limit < 1 {
		return ListMessagesOutput{}, &domainerrors.ValidationError{Field: "limit", Reason: "must be at least 1"}
	}
	rows, err := u.messages.List(ctx, repository.ListQuery{ProjectID: in.ProjectID, Limit: in.Limit + 1, After: in.After})
	if err != nil {
		return ListMessagesOutput{}, fmt.Errorf("list messages: %w", err)
	}
	page, next := trimPage(rows, in.Limit)
	if len(page) == 0 {
		return ListMessagesOutput{}, nil
	}
	summaries, err := u.deliveries.Summaries(ctx, in.ProjectID, headerIDs(page))
	if err != nil {
		return ListMessagesOutput{}, fmt.Errorf("summarize deliveries: %w", err)
	}
	return ListMessagesOutput{Items: zip(page, summaries), Next: next}, nil
}

func trimPage(rows []entity.MessageHeader, limit int) ([]entity.MessageHeader, *entity.PageAnchor) {
	if len(rows) <= limit {
		return rows, nil
	}
	page := rows[:limit]
	last := page[len(page)-1]
	return page, &entity.PageAnchor{CreatedAt: last.CreatedAt, ID: last.ID}
}

func headerIDs(headers []entity.MessageHeader) []string {
	ids := make([]string, 0, len(headers))
	for _, h := range headers {
		ids = append(ids, h.ID)
	}
	return ids
}

// zip pairs each message with its counts. A message the reader did not mention has none.
func zip(headers []entity.MessageHeader, summaries map[string]entity.DeliverySummary) []MessageListItem {
	items := make([]MessageListItem, 0, len(headers))
	for _, h := range headers {
		items = append(items, MessageListItem{Header: h, Summary: summaries[h.ID]})
	}
	return items
}
