package random_test

import (
	"testing"

	"hookrelay/internal/library/random"
)

func TestUniformSamplesStayInTheUnitInterval(t *testing.T) {
	const draws = 10000
	var u random.Uniform
	var sum float64
	first, allEqual := u.Float64(), true

	for range draws {
		f := u.Float64()
		if f < 0 || f >= 1 {
			t.Fatalf("sample %v outside [0, 1)", f)
		}
		allEqual = allEqual && f == first
		sum += f
	}

	if allEqual {
		t.Fatal("all samples are equal")
	}
	if mean := sum / draws; mean < 0.45 || mean > 0.55 {
		t.Fatalf("mean = %v, want 0.5 +- 0.05", mean)
	}
}
