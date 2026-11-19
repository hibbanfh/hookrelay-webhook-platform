package usecase_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/usecase"
)

func TestSummarizeDeliveries(t *testing.T) {
	counts := map[string]entity.StatusCounts{"msg_1": {Pending: 1, Failed: 2}}
	tests := []struct {
		name      string
		ids       []string
		counts    map[string]entity.StatusCounts
		err       error
		want      map[string]entity.StatusCounts
		wantCalls int
		wantErr   error
	}{
		{name: "no ids makes no query", ids: nil, want: map[string]entity.StatusCounts{}},
		{name: "every requested id is present", ids: []string{"msg_1", "msg_2"}, counts: counts,
			want: map[string]entity.StatusCounts{"msg_1": {Pending: 1, Failed: 2}, "msg_2": {}}, wantCalls: 1},
		{name: "reader error is wrapped", ids: []string{"msg_1"}, err: errStorage, wantErr: errStorage, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeDeliveryReader{counts: tt.counts, countErr: tt.err}
			uc := usecase.NewSummarizeDeliveries(reader)

			got, err := uc.Execute(context.Background(), usecase.SummarizeDeliveriesInput{ProjectID: "p", MessageIDs: tt.ids})

			if !errors.Is(err, tt.wantErr) || len(reader.countCalls) != tt.wantCalls {
				t.Fatalf("err %v, calls %d", err, len(reader.countCalls))
			}
			if tt.wantErr == nil && !maps.Equal(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
