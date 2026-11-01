package history

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// resetIDState forgets earlier ids so a test can pick its own timestamps.
func resetIDState() {
	idState.mu.Lock()
	defer idState.mu.Unlock()
	idState.ms, idState.entropy = 0, [10]byte{}
}

// decodeTime reads the 48-bit millisecond timestamp back out of a ULID.
func decodeTime(t *testing.T, id string) int64 {
	t.Helper()
	var ms int64
	for _, c := range id[:10] {
		i := strings.IndexRune(crockford, c)
		if i < 0 {
			t.Fatalf("%q: character %q is not Crockford base32", id, c)
		}
		ms = ms<<5 | int64(i)
	}
	return ms
}

func TestNewIDShapeAndTimestamp(t *testing.T) {
	resetIDState()
	at := time.Date(2026, 11, 1, 12, 34, 56, 789_000_000, time.UTC)
	id := NewID(at)
	if len(id) != 26 || id[0] > '7' {
		t.Fatalf("not a ULID: %q", id)
	}
	if got := decodeTime(t, id); got != at.UnixMilli() {
		t.Fatalf("timestamp %d, want %d", got, at.UnixMilli())
	}
	for _, c := range id {
		if !strings.ContainsRune(crockford, c) {
			t.Fatalf("%q has %q", id, c)
		}
	}
}

func TestIDsSortInCreationOrder(t *testing.T) {
	resetIDState()
	// Same millisecond: the random part is incremented, so order holds.
	at := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	var same []string
	for i := 0; i < 5000; i++ {
		same = append(same, NewID(at))
	}
	assertStrictlyIncreasing(t, same)

	// Across milliseconds, and even when the clock steps back.
	var later []string
	for i := 0; i < 50; i++ {
		later = append(later, NewID(at.Add(time.Duration(i+1)*time.Millisecond)))
	}
	later = append(later, NewID(at)) // clock went back: still after the previous id
	assertStrictlyIncreasing(t, later)
	if !sort.StringsAreSorted(later) {
		t.Fatal("ids are not lexicographically sorted")
	}
}

func TestIDsAreUniqueUnderConcurrency(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				id := NewID(time.Now())
				mu.Lock()
				if seen[id] {
					t.Errorf("duplicate id %s", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
}

func TestIDBeforeEpochDoesNotPanic(t *testing.T) {
	if id := NewID(time.Unix(-5, 0)); len(id) != 26 {
		t.Fatalf("%q", id)
	}
}

func assertStrictlyIncreasing(t *testing.T, ids []string) {
	t.Helper()
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("id %d (%s) does not sort after id %d (%s)", i, ids[i], i-1, ids[i-1])
		}
	}
}
