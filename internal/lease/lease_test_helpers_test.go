package lease

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// owner returns an owner tied to the live test process.
func owner(t *testing.T, lane, session string) Owner {
	t.Helper()
	o, err := NewOwner(lane, session, 0)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func readEvents(t *testing.T, s *Store) []Event {
	t.Helper()
	f, err := os.Open(s.eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var evs []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("bad event line %q: %v", sc.Text(), err)
		}
		evs = append(evs, e)
	}
	return evs
}

func opsOf(evs []Event) []string {
	var ops []string
	for _, e := range evs {
		ops = append(ops, e.Op)
	}
	return ops
}

func wantOps(t *testing.T, s *Store, want ...string) []Event {
	t.Helper()
	evs := readEvents(t, s)
	got := opsOf(evs)
	if len(got) != len(want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ops = %v, want %v", got, want)
		}
	}
	return evs
}
