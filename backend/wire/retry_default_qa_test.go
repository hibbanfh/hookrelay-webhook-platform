package wire_test

import (
	"math"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/library/random"
	"hookrelay/internal/platform/config"
	"hookrelay/wire"
)

// US-005 AC 1 through the real parts: the default configuration parsed by config.Load, the
// planner built by the Wire provider and the production jitter source. Every sampled retry time
// must stay within +-10% of 5s, 30s, 2m, 15m and 1h, and the spread must reach both ends.
func TestDefaultScheduleStaysWithinTenPercentJitter(t *testing.T) {
	cfg, err := config.Load(func(k string) string {
		return map[string]string{
			"APP_ENV": "development", "DATABASE_URL": "postgres://u:p@localhost/db",
			"SECRET_ENCRYPTION_KEY": "CQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQk=",
		}[k]
	})
	if err != nil {
		t.Fatalf("config.Load with the default retry settings: %v", err)
	}
	planner, err := wire.ProvideRetryPlanner(cfg, wire.ProvideDispatchJitter(random.NewUniform()))
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 15 * time.Minute, time.Hour}

	for i, base := range want {
		t.Run(base.String(), func(t *testing.T) {
			low, high := sampleRange(t, planner, i+1, 20000)

			if low < base*9/10 || high > base*11/10 {
				t.Fatalf("sampled range [%v, %v] leaves [%v, %v]", low, high, base*9/10, base*11/10)
			}
			if low > base*92/100 || high < base*108/100 {
				t.Fatalf("sampled range [%v, %v] does not reach the +-10%% ends of %v", low, high, base)
			}
		})
	}
	if _, ok := planner.Next(len(want)+1, sampleNow); ok {
		t.Fatal("a 6th failed attempt must not be retried")
	}
}

// sampleNow is the fixed failure time the samples are measured from.
var sampleNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// sampleRange draws the delay after failed attempt n many times and returns the extremes.
func sampleRange(t *testing.T, planner entity.RetryPlanner, n, draws int) (low, high time.Duration) {
	t.Helper()
	low = time.Duration(math.MaxInt64)
	for range draws {
		next, ok := planner.Next(n, sampleNow)
		if !ok {
			t.Fatalf("attempt %d must be retried", n)
		}
		got := next.Sub(sampleNow)
		low, high = min(low, got), max(high, got)
	}
	return low, high
}
