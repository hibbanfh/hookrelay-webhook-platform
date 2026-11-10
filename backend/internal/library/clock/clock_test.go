package clock_test

import (
	"testing"
	"time"

	"hookrelay/internal/library/clock"
)

func TestSystemNowIsUTCAndMicrosecondPrecise(t *testing.T) {
	for range 50 {
		now := clock.System{}.Now()

		if now.Location() != time.UTC || now.Nanosecond()%int(time.Microsecond) != 0 {
			t.Fatalf("got %v (%d ns), want UTC truncated to microseconds", now, now.Nanosecond())
		}
	}
}

func TestSystemNowTracksTheWallClock(t *testing.T) {
	before := time.Now().Add(-time.Microsecond)

	now := clock.System{}.Now()

	if now.Before(before) || time.Since(now) > time.Minute {
		t.Fatalf("clock is off: %v", now)
	}
}
