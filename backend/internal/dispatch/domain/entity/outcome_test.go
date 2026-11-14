package entity_test

import (
	"testing"

	"hookrelay/internal/dispatch/domain/entity"
)

func TestOutcomeIsSuccess(t *testing.T) {
	tests := []struct {
		name string
		out  entity.Outcome
		want bool
	}{
		{"199", entity.HTTPOutcome(199, nil), false},
		{"200", entity.HTTPOutcome(200, nil), true},
		{"299", entity.HTTPOutcome(299, nil), true},
		{"300", entity.HTTPOutcome(300, nil), false},
		{"302", entity.HTTPOutcome(302, nil), false},
		{"500", entity.HTTPOutcome(500, nil), false},
		{"an error with status 0", entity.FailedOutcome(entity.ErrorTimeout), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.out.IsSuccess(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAttemptErrorIsTerminal(t *testing.T) {
	tests := []struct {
		err  entity.AttemptError
		want bool
	}{
		{entity.ErrorDestinationBlocked, true},
		{entity.ErrorEndpointDisabled, true},
		{entity.ErrorTimeout, false},
		{entity.ErrorDNS, false},
		{entity.ErrorConnection, false},
		{entity.ErrorNone, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.err), func(t *testing.T) {
			if got := tt.err.IsTerminal(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAttemptDurationMSIsNeverNegative(t *testing.T) {
	tests := []struct {
		name string
		d    int64 // milliseconds
		want int
	}{{"positive", 1234, 1234}, {"zero", 0, 0}, {"clock stepped back", -50, 0}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := entity.Attempt{Duration: durationMS(tt.d)}
			if got := a.DurationMS(); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}
