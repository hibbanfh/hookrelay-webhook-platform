// Package ids generates prefixed, time-sortable identifiers (prefix + ULID).
package ids

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

const (
	crockford   = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	timeBytes   = 6
	randomBytes = 10
	ulidBytes   = timeBytes + randomBytes
	ulidChars   = 26
	bitsPerChar = 5
	padBits     = ulidChars*bitsPerChar - ulidBytes*8
)

// Clock provides the current time.
type Clock interface{ Now() time.Time }

// Generator produces ULID based IDs that are strictly increasing within one process.
type Generator struct {
	mu      sync.Mutex
	clock   Clock
	entropy func([]byte)
	lastMS  uint64
	lastRnd [randomBytes]byte
}

// NewGenerator builds a Generator. entropy fills the slice with random bytes.
func NewGenerator(clock Clock, entropy func([]byte)) *Generator {
	return &Generator{clock: clock, entropy: entropy}
}

// CryptoEntropy fills p from crypto/rand.
func CryptoEntropy(p []byte) {
	// Since Go 1.24 rand.Read never returns an error: failure crashes the program
	// irrecoverably, so there is nothing to handle here.
	_, _ = rand.Read(p)
}

// New returns prefix followed by a 26 character ULID.
func (g *Generator) New(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	ms := uint64(g.clock.Now().UnixMilli()) //nolint:gosec // pre-1970 clocks are not supported
	g.advance(ms)
	return prefix + encode(g.lastMS, g.lastRnd)
}

// advance moves to a fresh random part, or increments it when the millisecond did not
// advance, so IDs stay ordered even if the clock stalls or steps backwards.
func (g *Generator) advance(ms uint64) {
	if ms <= g.lastMS && g.lastMS != 0 {
		g.increment()
		return
	}
	g.lastMS = ms
	g.entropy(g.lastRnd[:])
}

func (g *Generator) increment() {
	// The 80-bit counter is a uint16 high part and a uint64 low part, carried by hand.
	low := binary.BigEndian.Uint64(g.lastRnd[2:]) + 1
	binary.BigEndian.PutUint64(g.lastRnd[2:], low)
	if low != 0 {
		return
	}
	binary.BigEndian.PutUint16(g.lastRnd[:2], binary.BigEndian.Uint16(g.lastRnd[:2])+1)
}

func encode(ms uint64, rnd [randomBytes]byte) string {
	var raw [ulidBytes]byte
	for i := 0; i < timeBytes; i++ {
		raw[i] = byte(ms >> (8 * (timeBytes - 1 - i)))
	}
	copy(raw[timeBytes:], rnd[:])
	var out [ulidChars]byte
	for i := range out {
		out[i] = crockford[fiveBits(raw, i)]
	}
	return string(out[:])
}

// fiveBits returns the i-th 5-bit group of the big-endian stream, left padded with zero bits.
func fiveBits(raw [ulidBytes]byte, i int) byte {
	var v byte
	for k := 0; k < bitsPerChar; k++ {
		pos := i*bitsPerChar + k - padBits
		v = v<<1 | bitAt(raw, pos)
	}
	return v
}

func bitAt(raw [ulidBytes]byte, pos int) byte {
	if pos < 0 {
		return 0
	}
	return raw[pos/8] >> (7 - pos%8) & 1
}
