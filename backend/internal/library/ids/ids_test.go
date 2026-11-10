package ids_test

import (
	"sort"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/library/ids"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func fill(b byte) func([]byte) {
	return func(p []byte) {
		for i := range p {
			p[i] = b
		}
	}
}

func TestGeneratorEncoding(t *testing.T) {
	tests := []struct {
		name    string
		now     time.Time
		entropy func([]byte)
		want    string
	}{
		{"zero time and entropy", time.UnixMilli(0), fill(0), "proj_" + strings.Repeat("0", 26)},
		{"zero time, max entropy", time.UnixMilli(0), fill(0xFF), "proj_" + strings.Repeat("0", 10) + strings.Repeat("Z", 16)},
		{"one millisecond", time.UnixMilli(1), fill(0), "proj_" + strings.Repeat("0", 9) + "1" + strings.Repeat("0", 16)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := ids.NewGenerator(fixedClock{tt.now}, tt.entropy)
			if got := g.New("proj_"); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGeneratorIsMonotonicWithinOneMillisecond(t *testing.T) {
	g := ids.NewGenerator(fixedClock{time.UnixMilli(1_700_000_000_000)}, fill(0))
	got := make([]string, 0, 50)
	for range 50 {
		got = append(got, g.New("ep_"))
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("ids not increasing: %v", got)
	}
	if got[0] == got[1] {
		t.Fatal("ids must be unique")
	}
}

func TestGeneratorSurvivesClockGoingBackwards(t *testing.T) {
	clk := &steppingClock{ms: []int64{2000, 1000}}
	g := ids.NewGenerator(clk, fill(0))
	first, second := g.New("x_"), g.New("x_")
	if second <= first {
		t.Fatalf("second %s must sort after first %s", second, first)
	}
}

type steppingClock struct {
	ms []int64
	i  int
}

func (c *steppingClock) Now() time.Time {
	t := time.UnixMilli(c.ms[c.i])
	c.i++
	return t
}
