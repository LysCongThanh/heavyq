package history

import (
	"crypto/rand"
	"sync"
	"time"
)

// crockford is the ULID alphabet: digits and letters without I, L, O, U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// idState keeps ids created by one process strictly increasing, even inside
// one millisecond or when the clock steps back.
var idState struct {
	mu      sync.Mutex
	ms      uint64
	entropy [10]byte
}

// NewID returns a ULID for time t: 26 characters of Crockford base32, a 48-bit
// millisecond timestamp followed by 80 random bits. Ids sort lexicographically
// in creation order; two ids made by the same process always do, because the
// random part is incremented instead of redrawn within one millisecond.
func NewID(t time.Time) string {
	ms := uint64(max(t.UnixMilli(), 0)) & (1<<48 - 1)
	idState.mu.Lock()
	defer idState.mu.Unlock()
	if ms <= idState.ms && idState.ms != 0 {
		ms = idState.ms
		if !increment(&idState.entropy) { // 2^80 ids in one millisecond: move on
			ms++
			redraw(&idState.entropy)
		}
	} else {
		redraw(&idState.entropy)
	}
	idState.ms = ms

	var raw [16]byte
	for i := 0; i < 6; i++ {
		raw[i] = byte(ms >> (8 * (5 - i)))
	}
	copy(raw[6:], idState.entropy[:])
	return encodeULID(raw)
}

// redraw fills e with fresh random bytes. crypto/rand.Read does not fail on
// supported platforms (it aborts the process instead), so there is no error to
// handle.
func redraw(e *[10]byte) { _, _ = rand.Read(e[:]) }

// increment adds one to e as a big-endian integer; false on overflow.
func increment(e *[10]byte) bool {
	for i := len(e) - 1; i >= 0; i-- {
		e[i]++
		if e[i] != 0 {
			return true
		}
	}
	return false
}

// encodeULID writes the 128 bits of raw as 26 base32 characters (the first
// carries only 3 bits, so it is at most '7').
func encodeULID(raw [16]byte) string {
	var out [26]byte
	for i := range out {
		var v byte
		for k := 0; k < 5; k++ {
			pos := i*5 + k - 2 // bit index from the most significant bit
			v <<= 1
			if pos >= 0 && raw[pos/8]&(0x80>>(pos%8)) != 0 {
				v |= 1
			}
		}
		out[i] = crockford[v]
	}
	return string(out[:])
}
