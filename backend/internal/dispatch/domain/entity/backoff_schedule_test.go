package entity_test

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
)

// fixedJitter is a deterministic JitterSource.
type fixedJitter struct{ u float64 }

func (f fixedJitter) Float64() float64 { return f.u }

var defaultDelays = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 15 * time.Minute, time.Hour}

func mustSchedule(t *testing.T, delays []time.Duration, jitter, u float64) entity.BackoffSchedule {
	t.Helper()
	s, err := entity.NewBackoffSchedule(delays, jitter, fixedJitter{u: u})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBackoffScheduleDefaultDelays(t *testing.T) {
	s := mustSchedule(t, defaultDelays, 0.10, 0.5)

	for i, d := range defaultDelays {
		got, ok := s.Next(i+1, t0)
		if !ok || !got.Equal(t0.Add(d)) {
			t.Fatalf("Next(%d) = %v, %v; want %v", i+1, got, ok, t0.Add(d))
		}
	}
}

func TestBackoffScheduleExhaustedOrOutOfRange(t *testing.T) {
	s := mustSchedule(t, defaultDelays, 0.10, 0.5)
	for _, n := range []int{-1, 0, 6, 100} {
		if _, ok := s.Next(n, t0); ok {
			t.Fatalf("Next(%d) must report no retry", n)
		}
	}
}

func TestBackoffScheduleJitterBounds(t *testing.T) {
	d := 100 * time.Second
	tests := []struct {
		name     string
		jitter   float64
		u        float64
		min, max time.Duration // inclusive bounds on the delay
	}{
		{name: "lowest sample is 0.9 d", jitter: 0.10, u: 0, min: 90 * time.Second, max: 90 * time.Second},
		{name: "highest sample is just below 1.1 d", jitter: 0.10, u: 0.999999, min: time.Duration(1.0999 * float64(d)), max: 110*time.Second - 1},
		{name: "no jitter is exact whatever the sample", jitter: 0, u: 0.77, min: d, max: d},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mustSchedule(t, []time.Duration{d}, tt.jitter, tt.u).Next(1, t0)

			delay := got.Sub(t0)
			if !ok || delay < tt.min || delay > tt.max {
				t.Fatalf("delay = %v (ok=%v), want within [%v, %v]", delay, ok, tt.min, tt.max)
			}
		})
	}
}

func TestBackoffScheduleEmptyNeverRetries(t *testing.T) {
	s := mustSchedule(t, nil, 0.10, 0.5)

	if _, ok := s.Next(1, t0); ok {
		t.Fatal("an empty schedule must never retry")
	}
}

func TestNewBackoffScheduleRejectsInvalidPolicy(t *testing.T) {
	ok := []time.Duration{time.Second}
	tests := []struct {
		name   string
		delays []time.Duration
		jitter float64
		src    entity.JitterSource
	}{
		{name: "zero delay", delays: []time.Duration{0}, src: fixedJitter{}},
		{name: "negative delay", delays: []time.Duration{-time.Second}, src: fixedJitter{}},
		{name: "delay above the maximum", delays: []time.Duration{entity.MaxRetryDelay + time.Nanosecond}, src: fixedJitter{}},
		{name: "negative jitter", delays: ok, jitter: -0.01, src: fixedJitter{}},
		{name: "jitter of one", delays: ok, jitter: 1, src: fixedJitter{}},
		{name: "NaN jitter", delays: ok, jitter: math.NaN(), src: fixedJitter{}},
		{name: "nil source", delays: ok, jitter: 0.1, src: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entity.NewBackoffSchedule(tt.delays, tt.jitter, tt.src)

			if !errors.Is(err, domainerrors.ErrInvalidRetryPolicy) {
				t.Fatalf("err = %v, want ErrInvalidRetryPolicy", err)
			}
		})
	}
}

func TestNewBackoffScheduleCopiesTheDelays(t *testing.T) {
	delays := slices.Clone(defaultDelays[:1])
	s := mustSchedule(t, delays, 0, 0.5)

	delays[0] = time.Hour

	got, _ := s.Next(1, t0)
	if !got.Equal(t0.Add(5 * time.Second)) {
		t.Fatalf("schedule changed with the caller's slice: %v", got)
	}
}
