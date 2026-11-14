package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/dispatch/delivery/worker"
)

func TestHeartbeatPing(t *testing.T) {
	start := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		elapsed time.Duration
		beatAt  time.Duration // when Beat is called again, relative to start; negative means never
		wantErr error
	}{
		{name: "healthy right after start", elapsed: 0, beatAt: -1},
		{name: "healthy within the max age", elapsed: 29 * time.Second, beatAt: -1},
		{name: "stale after the max age", elapsed: 31 * time.Second, beatAt: -1, wantErr: worker.ErrStale},
		{name: "healthy again after a beat", elapsed: 31 * time.Second, beatAt: 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := start
			hb := worker.NewHeartbeat(func() time.Time { return now }, 30*time.Second)
			if tt.beatAt >= 0 {
				now = start.Add(tt.beatAt)
				hb.Beat()
			}
			now = start.Add(tt.elapsed)

			if err := hb.Ping(context.Background()); !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}
