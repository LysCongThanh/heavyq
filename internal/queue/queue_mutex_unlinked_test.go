package queue

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A waiter polling .mutex retries on the inode it opened. When a temp cleaner
// unlinks the file meanwhile, that waiter ends up holding a lock nobody else
// can see: the next round creates a fresh .mutex and runs at the same time.
// The waiter must notice and lock the file at the path instead.
func TestMutexWaiterRelocksAfterTheFileWasUnlinked(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unlockA, err := d.Mutex(5*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	bHeld := make(chan func(), 1)
	go func() {
		u, err := d.Mutex(5*time.Second, nil)
		if err != nil {
			t.Error(err)
			u = func() {}
		}
		bHeld <- u
	}()
	time.Sleep(200 * time.Millisecond) // B is retrying flock on the old inode
	if err := os.Remove(filepath.Join(d.Path, ".mutex")); err != nil {
		t.Fatal(err)
	}
	unlockA()
	var unlockB func()
	select {
	case unlockB = <-bHeld:
	case <-time.After(5 * time.Second):
		t.Fatal("waiter never got the mutex")
	}
	cHeld := make(chan func(), 1)
	go func() {
		u, err := d.Mutex(5*time.Second, nil)
		if err == nil {
			cHeld <- u
		}
	}()
	select {
	case u := <-cHeld:
		u()
		unlockB()
		t.Fatal("two rounds hold the queue mutex at once")
	case <-time.After(300 * time.Millisecond):
	}
	unlockB()
	select {
	case u := <-cHeld:
		u()
	case <-time.After(5 * time.Second):
		t.Fatal("next round never got the mutex")
	}
}
