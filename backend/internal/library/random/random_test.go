package random_test

import (
	"bytes"
	"testing"

	"hookrelay/internal/library/random"
)

func TestBytes(t *testing.T) {
	var src random.Crypto
	a, errA := src.Bytes(32)
	b, errB := src.Bytes(32)
	if errA != nil || errB != nil || len(a) != 32 || len(b) != 32 {
		t.Fatalf("unexpected result: %v %v %d %d", errA, errB, len(a), len(b))
	}
	if bytes.Equal(a, b) {
		t.Fatal("two reads must differ")
	}
}
