package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/usecase"
)

func TestListMessages(t *testing.T) {
	summaries := map[string]entity.DeliverySummary{"msg_0": {Pending: 1, Failed: 2}, "msg_1": {Succeeded: 3}}
	tests := []struct {
		name         string
		limit        int
		headers      []entity.MessageHeader
		listErr      error
		summaryErr   error
		wantInvalid  bool
		wantErr      error
		wantItems    int
		wantNext     string
		wantSummarys int
		wantFetch    int
	}{
		{name: "limit below one", limit: 0, wantInvalid: true},
		{name: "list error is wrapped", limit: 2, listErr: errBoom, wantErr: errBoom, wantFetch: 3},
		{name: "empty page makes no summary query", limit: 2, wantFetch: 3},
		{name: "exactly limit rows has no next", limit: 2, headers: headersAt(2), wantItems: 2, wantSummarys: 1, wantFetch: 3},
		{name: "limit plus one rows has next", limit: 2, headers: headersAt(3), wantItems: 2, wantNext: "msg_1", wantSummarys: 1, wantFetch: 3},
		{name: "summary error is wrapped", limit: 2, headers: headersAt(2), summaryErr: errBoom, wantErr: errBoom, wantSummarys: 1, wantFetch: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &readMessages{fakeMessages: &fakeMessages{}, headers: tt.headers, listErr: tt.listErr}
			deliveries := &fakeDeliveries{summaries: summaries, summariesErr: tt.summaryErr}
			uc := usecase.NewListMessages(repo, deliveries)

			out, err := uc.Execute(context.Background(), usecase.ListMessagesInput{ProjectID: "p", Limit: tt.limit})

			assertReadErr(t, err, tt.wantErr, tt.wantInvalid)
			if len(out.Items) != tt.wantItems || anchorID(out.Next) != tt.wantNext {
				t.Fatalf("items %d next %q, want %d / %q", len(out.Items), anchorID(out.Next), tt.wantItems, tt.wantNext)
			}
			if len(deliveries.summaryCalls) != tt.wantSummarys {
				t.Fatalf("summary queries = %d, want exactly %d (no N+1)", len(deliveries.summaryCalls), tt.wantSummarys)
			}
			if tt.wantFetch > 0 && repo.listCalls[0].Limit != tt.wantFetch {
				t.Fatalf("fetched %d, want limit+1 = %d", repo.listCalls[0].Limit, tt.wantFetch)
			}
		})
	}
}

func TestListMessagesZipsSummariesAndFillsMissingWithZeros(t *testing.T) {
	repo := &readMessages{fakeMessages: &fakeMessages{}, headers: headersAt(2)}
	deliveries := &fakeDeliveries{summaries: map[string]entity.DeliverySummary{"msg_0": {Pending: 1, Failed: 2}}}
	uc := usecase.NewListMessages(repo, deliveries)

	out, err := uc.Execute(context.Background(), usecase.ListMessagesInput{ProjectID: "p", Limit: 5})

	if err != nil || len(out.Items) != 2 {
		t.Fatalf("items %d, err %v", len(out.Items), err)
	}
	if out.Items[0].Summary != (entity.DeliverySummary{Pending: 1, Failed: 2}) || out.Items[1].Summary != (entity.DeliverySummary{}) {
		t.Fatalf("summaries = %+v / %+v", out.Items[0].Summary, out.Items[1].Summary)
	}
	if got := deliveries.summaryCalls[0]; len(got) != 2 || got[0] != "msg_0" || got[1] != "msg_1" {
		t.Fatalf("summaries asked for %v, want the IDs of the page only", got)
	}
}

func TestListMessagesPageIDsExcludeTheProbeRow(t *testing.T) {
	repo := &readMessages{fakeMessages: &fakeMessages{}, headers: headersAt(3)}
	deliveries := &fakeDeliveries{}
	uc := usecase.NewListMessages(repo, deliveries)

	_, err := uc.Execute(context.Background(), usecase.ListMessagesInput{ProjectID: "p", Limit: 2})

	if got := deliveries.summaryCalls[0]; err != nil || len(got) != 2 {
		t.Fatalf("err %v, summaries asked for %v", err, got)
	}
}

func assertReadErr(t *testing.T, err, wantErr error, wantInvalid bool) {
	t.Helper()
	if wantInvalid {
		var invalid *domainerrors.ValidationError
		if !errors.As(err, &invalid) {
			t.Fatalf("err = %v, want a validation error", err)
		}
		return
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func anchorID(a *entity.PageAnchor) string {
	if a == nil {
		return ""
	}
	return a.ID
}
