package slots

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// Busy is called by status, dash and why without the queue mutex, and by
// pools.Check in front of every real Acquire. It must never take the lock
// itself: a real admission (or a legacy `flock -n` user) that lands inside
// such a probe would see a free slot as busy.
func TestBusyNeverTakesAFreeSlot(t *testing.T) {
	p := filepath.Join(t.TempDir(), "slots", "heavy-1.lock")
	l, err := TryLock(p)
	if err != nil || l == nil {
		t.Fatalf("create: %v %v", l, err)
	}
	l.Release()

	var stop atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ { // two dashboards refreshing
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				Busy(p)
			}
		}()
	}
	refused := 0
	for i := 0; i < 5000; i++ {
		l, err := TryLock(p)
		if err != nil {
			t.Fatal(err)
		}
		if l == nil {
			refused++
			continue
		}
		l.Release()
	}
	stop.Store(true)
	wg.Wait()
	if refused > 0 {
		t.Fatalf("a free slot was refused %d times while Busy probed it", refused)
	}
}

// Busy must still see every kind of holder: a heavyq lock in this process,
// one in another process (flock(1)), and a released lock as free.
func TestBusySeesHoldersWithoutLocking(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s", "light-1.lock")
	if Busy(p) {
		t.Fatal("a missing file is not busy")
	}
	l, err := TryLock(p)
	if err != nil || l == nil {
		t.Fatalf("lock: %v %v", l, err)
	}
	for i := 0; i < 3; i++ {
		if !Busy(p) {
			t.Fatal("Busy missed a held lock")
		}
	}
	l.Release()
	if Busy(p) {
		t.Fatal("released lock still busy")
	}
}
