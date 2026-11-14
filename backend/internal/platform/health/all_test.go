package health_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/platform/health"
)

type pinger struct {
	err   error
	calls int
}

func (p *pinger) Ping(context.Context) error {
	p.calls++
	return p.err
}

func TestAll(t *testing.T) {
	errFirst, errSecond := errors.New("first"), errors.New("second")
	tests := []struct {
		name      string
		members   []*pinger
		wantErr   error
		wantCalls []int
	}{
		{name: "no members is healthy", members: nil, wantCalls: nil},
		{name: "all healthy", members: []*pinger{{}, {}}, wantCalls: []int{1, 1}},
		{name: "the first error wins and stops the rest", members: []*pinger{{err: errFirst}, {err: errSecond}}, wantErr: errFirst, wantCalls: []int{1, 0}},
		{name: "a later member can fail", members: []*pinger{{}, {err: errSecond}}, wantErr: errSecond, wantCalls: []int{1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			members := make([]health.Pinger, 0, len(tt.members))
			for _, m := range tt.members {
				members = append(members, m)
			}

			err := health.All(members...).Ping(context.Background())

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			for i, m := range tt.members {
				if m.calls != tt.wantCalls[i] {
					t.Fatalf("member %d pinged %d times, want %d", i, m.calls, tt.wantCalls[i])
				}
			}
		})
	}
}
