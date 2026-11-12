package slots

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Busy is called by status, dash and why without the queue mutex, and by
// pools.Check in front of every real Acquire. It must never take the lock
// itself: a real admission (or a legacy `flock -n` user) that lands inside
// such a probe would see a free slot as busy.
func TestBusyNeverTakesAFreeSlot(t *testing.T) {
	needKernelLook(t)
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

// A legacy `flock -n <file> cmd` user must get a free slot while status or
// the dashboard keep probing it.
func TestLegacyFlockNSucceedsWhileBusyProbes(t *testing.T) {
	needKernelLook(t)
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) not installed")
	}
	p := filepath.Join(t.TempDir(), "heavy-1.lock")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			Busy(p)
		}
	}()
	defer func() { stop.Store(true); <-done }()
	for i := 0; i < 40; i++ {
		if err := exec.Command("flock", "-n", p, "true").Run(); err != nil {
			t.Fatalf("flock -n failed on a free slot (attempt %d): %v", i, err)
		}
	}
}

// On Linux and macOS the kernel answers; the take-and-release probe is only
// the fallback, so it must not be what answers here.
func TestHeldByAnyoneAnswersWithoutTheProbe(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no lock-free look on " + runtime.GOOS)
	}
	p := filepath.Join(t.TempDir(), "heavy-1.lock")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// In a pid namespace of its own (a container) /proc/locks cannot vouch
	// for "free", so only the initial namespace must answer it.
	if busy, known := heldByAnyone(p); busy || (!known && kernelSeesHolders()) {
		t.Fatalf("free file: busy=%v known=%v", busy, known)
	}
	l, err := TryLock(p)
	if err != nil || l == nil {
		t.Fatalf("lock: %v %v", l, err)
	}
	if busy, known := heldByAnyone(p); !known || !busy {
		t.Fatalf("held file: busy=%v known=%v", busy, known)
	}
	l.Release()
	if _, err := exec.LookPath("flock"); err != nil {
		return
	}
	cmd := exec.Command("flock", p, "sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if busy, known := heldByAnyone(p); known && busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("heldByAnyone never saw flock(1) hold the file")
		}
		time.Sleep(20 * time.Millisecond)
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

// kernelSeesHolders reports whether heldByAnyone can vouch for a free file
// here: macOS, or Linux in the initial pid namespace. Anywhere else Busy
// falls back to taking and releasing the lock, which has the very race the
// tests above rule out.
func kernelSeesHolders() bool {
	switch runtime.GOOS {
	case "darwin":
		return true
	case "linux":
		ns, err := os.Readlink("/proc/self/ns/pid")
		return err == nil && ns == "pid:[4026531836]"
	}
	return false
}

func needKernelLook(t *testing.T) {
	if !kernelSeesHolders() {
		t.Skip("no lock-free look here (another OS or a pid namespace of its own); Busy probes")
	}
}
