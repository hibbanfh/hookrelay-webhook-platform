package config_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/platform/config"
)

var defaultRetryDelays = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 15 * time.Minute, time.Hour}

func retryEnv(schedule, jitter string) config.Getenv {
	m := validEnv()
	m["RETRY_SCHEDULE"], m["RETRY_JITTER"] = schedule, jitter
	return env(m)
}

func TestLoadRetrySettingsValid(t *testing.T) {
	tests := []struct {
		name       string
		schedule   string
		jitter     string
		want       []time.Duration
		wantJitter float64
	}{
		{name: "defaults", want: defaultRetryDelays, wantJitter: 0.10},
		{name: "two delays", schedule: "1s,2s", want: []time.Duration{time.Second, 2 * time.Second}, wantJitter: 0.10},
		{name: "spaces are trimmed", schedule: " 1s , 2s ", want: []time.Duration{time.Second, 2 * time.Second}, wantJitter: 0.10},
		{name: "empty parts are dropped", schedule: "1s,,2s", want: []time.Duration{time.Second, 2 * time.Second}, wantJitter: 0.10},
		{name: "24h is the maximum", schedule: "24h", want: []time.Duration{24 * time.Hour}, wantJitter: 0.10},
		{name: "jitter zero", jitter: "0", want: defaultRetryDelays, wantJitter: 0},
		{name: "jitter just below one", jitter: "0.99", want: defaultRetryDelays, wantJitter: 0.99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load(retryEnv(tt.schedule, tt.jitter))

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.RetryJitter != tt.wantJitter {
				t.Fatalf("jitter = %v, want %v", cfg.RetryJitter, tt.wantJitter)
			}
			if !slices.Equal(cfg.RetrySchedule, tt.want) {
				t.Fatalf("schedule = %v, want %v", cfg.RetrySchedule, tt.want)
			}
		})
	}
}

func TestLoadRetrySettingsInvalid(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		jitter   string
		wantErr  string
	}{
		{name: "only separators", schedule: ",", wantErr: "RETRY_SCHEDULE: must list at least one positive Go duration"},
		{name: "zero delay", schedule: "0s", wantErr: "RETRY_SCHEDULE"},
		{name: "negative delay", schedule: "-1s", wantErr: "RETRY_SCHEDULE"},
		{name: "not a duration", schedule: "abc", wantErr: `RETRY_SCHEDULE: invalid duration "abc"`},
		{name: "above 24h", schedule: "1s,25h", wantErr: `RETRY_SCHEDULE: "25h" exceeds 24h`},
		{name: "jitter one", jitter: "1", wantErr: "RETRY_JITTER: must satisfy 0 <= j < 1"},
		{name: "jitter negative", jitter: "-0.1", wantErr: "RETRY_JITTER"},
		{name: "jitter NaN", jitter: "NaN", wantErr: "RETRY_JITTER"},
		{name: "jitter Inf", jitter: "Inf", wantErr: "RETRY_JITTER"},
		{name: "jitter not a number", jitter: "abc", wantErr: "RETRY_JITTER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(retryEnv(tt.schedule, tt.jitter))

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
