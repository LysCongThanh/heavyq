package flowstate

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

var base = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

// clock is a test clock the store reads and the test moves.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// counting is a compute function that stamps the snapshot with the test clock and counts its calls.
func counting(c *clock, calls *atomic.Int32) func() (flow.Snapshot, error) {
	return func() (flow.Snapshot, error) {
		calls.Add(1)
		return flow.Build(flow.Input{Now: c.now(), Repo: "/r"}), nil
	}
}

func TestRefreshDebouncesToOneWritePerMinute(t *testing.T) {
	c := &clock{t: base}
	s := Store{Common: t.TempDir(), Now: c.now}
	var calls atomic.Int32
	compute := counting(c, &calls)

	if wrote, err := s.Refresh(false, compute); err != nil || !wrote {
		t.Fatalf("the first refresh writes: %v %v", wrote, err)
	}
	for _, step := range []time.Duration{time.Second, 20 * time.Second, 38 * time.Second} { // 59 s in total
		c.advance(step)
		if wrote, err := s.Refresh(false, compute); err != nil || wrote {
			t.Fatalf("within the debounce nothing is written or even computed: %v %v", wrote, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("compute ran %d times inside the debounce", calls.Load())
	}
	c.advance(time.Second) // 60 s
	if wrote, err := s.Refresh(false, compute); err != nil || !wrote || calls.Load() != 2 {
		t.Fatalf("after 60 s it writes again: %v %v calls=%d", wrote, err, calls.Load())
	}
	// A forced refresh (a person is needed) ignores the debounce.
	if wrote, err := s.Refresh(true, compute); err != nil || !wrote || calls.Load() != 3 {
		t.Fatalf("force writes at once: %v %v calls=%d", wrote, err, calls.Load())
	}
	snap, err := Read(s.Common)
	if err != nil || snap == nil || !snap.Updated.Equal(c.now()) {
		t.Fatalf("%+v %v", snap, err)
	}
}

func TestRefreshTreatsAStateFromTheFutureAsStale(t *testing.T) {
	c := &clock{t: base}
	s := Store{Common: t.TempDir(), Now: c.now}
	var calls atomic.Int32
	_, _ = s.Refresh(false, counting(c, &calls))
	c.advance(-time.Hour) // the clock stepped back
	if wrote, _ := s.Refresh(false, counting(c, &calls)); !wrote {
		t.Fatal("a state stamped after now must not block refreshes for an hour")
	}
}

// Many processes' worth of concurrent routine refreshes: exactly one computes and writes, under the flock. The
// others see the lock busy or the state fresh and do nothing.
func TestConcurrentRefreshesWriteOnce(t *testing.T) {
	c := &clock{t: base}
	common := t.TempDir()
	var calls atomic.Int32
	slow := func() (flow.Snapshot, error) { // slow enough that the others arrive while it holds the lock
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		return flow.Build(flow.Input{Now: c.now(), Repo: "/r"}), nil
	}
	var wg sync.WaitGroup
	var wrote atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine opens its own Store and lock file, like separate processes do.
			if w, err := (Store{Common: common, Now: c.now}).Refresh(false, slow); err != nil {
				t.Error(err)
			} else if w {
				wrote.Add(1)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 || wrote.Load() != 1 {
		t.Fatalf("compute ran %d times and %d refreshes wrote, want exactly 1", calls.Load(), wrote.Load())
	}
}

func TestForcedRefreshesSerialiseAndNeverTearTheFile(t *testing.T) {
	c := &clock{t: base}
	s := Store{Common: t.TempDir(), Now: c.now}
	var calls atomic.Int32
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 3; i++ { // readers see the old or the new state, never half of one
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := Read(s.Common); err != nil {
					t.Errorf("torn read: %v", err)
					return
				}
			}
		}()
	}
	var writers sync.WaitGroup
	for i := 0; i < 6; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			if _, err := s.Refresh(true, counting(c, &calls)); err != nil {
				t.Error(err)
			}
		}()
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	if calls.Load() != 6 {
		t.Fatalf("every forced refresh writes: %d", calls.Load())
	}
	entries, _ := os.ReadDir(Dir(s.Common))
	for _, e := range entries {
		switch e.Name() {
		case "state.json", "state.lock":
		default:
			t.Errorf("stray file %s: a temp file outlived its rename", e.Name())
		}
	}
}

func TestReadWithoutStateIsNilNotAnError(t *testing.T) {
	if s, err := Read(t.TempDir()); s != nil || err != nil {
		t.Fatalf("%v %v", s, err)
	}
}

func TestReadRefusesACorruptFileInsteadOfGuessing(t *testing.T) {
	common := t.TempDir()
	_ = os.MkdirAll(Dir(common), 0o700)
	_ = os.WriteFile(StatePath(common), []byte("{not json"), 0o600)
	if _, err := Read(common); err == nil {
		t.Fatal("a corrupt state must be an error")
	}
}

// Readers are on hot paths (the status line, top, the brief): reading a realistic state, 40 lanes and 25 needs-you
// entries, must take far under 5 ms.
func TestReaderLatencyUnderFiveMilliseconds(t *testing.T) {
	c := &clock{t: base}
	s := Store{Common: t.TempDir(), Now: c.now}
	snap := flow.Build(flow.Input{Now: base, Repo: "/r"})
	for i := 0; i < 40; i++ {
		snap.Lanes = append(snap.Lanes, flow.LaneState{Lane: fmt.Sprintf("lane-%02d", i), State: flow.Working, Item: fmt.Sprintf("item-%02d", i), Since: base})
	}
	for i := 0; i < 25; i++ {
		snap.NeedsYou = append(snap.NeedsYou, flow.NeedsYou{Item: "x", Lane: "l", Session: "s", Reason: "permission_prompt: Bash(npm run e2e)", Since: base})
	}
	if _, err := s.Refresh(true, func() (flow.Snapshot, error) { return snap, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(s.Common); err != nil { // warm the page cache
		t.Fatal(err)
	}
	const n = 200
	durs := make([]time.Duration, n)
	for i := range durs {
		start := time.Now()
		got, err := Read(s.Common)
		durs[i] = time.Since(start)
		if err != nil || got == nil || len(got.Lanes) != 40 {
			t.Fatalf("%v %v", got, err)
		}
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	// The median is the contract (one scheduling blip must not fail the suite); the 95th percentile is held to the same bar.
	if p50, p95 := durs[n/2], durs[n*95/100]; p50 > 5*time.Millisecond || p95 > 5*time.Millisecond {
		t.Errorf("read latency p50 %v p95 %v, want under 5 ms (worst %v)", p50, p95, durs[n-1])
	}
}
