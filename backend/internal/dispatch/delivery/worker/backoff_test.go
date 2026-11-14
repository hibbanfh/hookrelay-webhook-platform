package worker

import (
	"testing"
	"time"
)

func TestBackoffDoublesFromOneHundredMillisecondsUpToFiveSecondsAndResets(t *testing.T) {
	b := newBackoff()
	want := []time.Duration{
		100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond,
		1600 * time.Millisecond, 3200 * time.Millisecond, 5 * time.Second, 5 * time.Second,
	}

	for i, w := range want {
		if got := b.next(); got != w {
			t.Fatalf("wait %d = %v, want %v", i+1, got, w)
		}
	}
	b.reset()

	if got := b.next(); got != 100*time.Millisecond {
		t.Fatalf("after a success the wait is %v, want 100ms", got)
	}
}
