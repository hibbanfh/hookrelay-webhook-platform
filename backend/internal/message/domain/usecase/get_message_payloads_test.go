package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/usecase"
)

type payloadReader struct {
	rows  []entity.Message
	err   error
	calls int
}

func (p *payloadReader) Create(context.Context, entity.Message) error { return nil }

func (p *payloadReader) Get(context.Context, string, string) (entity.Message, error) {
	return entity.Message{}, nil
}

func (p *payloadReader) GetMany(context.Context, []string) ([]entity.Message, error) {
	p.calls++
	return p.rows, p.err
}

func TestGetMessagePayloads(t *testing.T) {
	boom := errors.New("boom")
	rows := []entity.Message{{ID: "msg_1", Payload: []byte(`{"a":1}`)}}
	tests := []struct {
		name      string
		ids       []string
		reader    *payloadReader
		wantCalls int
		wantLen   int
		wantErr   error
	}{
		{name: "returns the messages", ids: []string{"msg_1"}, reader: &payloadReader{rows: rows}, wantCalls: 1, wantLen: 1},
		{name: "empty input makes no query", reader: &payloadReader{rows: rows}},
		{name: "wraps the reader error", ids: []string{"msg_1"}, reader: &payloadReader{err: boom}, wantCalls: 1, wantErr: boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := usecase.NewGetMessagePayloads(tt.reader).Execute(context.Background(), usecase.GetMessagePayloadsInput{IDs: tt.ids})

			if !errors.Is(err, tt.wantErr) || tt.reader.calls != tt.wantCalls || len(got) != tt.wantLen {
				t.Fatalf("err %v, calls %d, messages %d", err, tt.reader.calls, len(got))
			}
		})
	}
}
