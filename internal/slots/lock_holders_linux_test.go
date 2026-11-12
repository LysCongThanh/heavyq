//go:build linux

package slots

import (
	"reflect"
	"testing"
)

func TestLocksOnInodeSkipsWaitersAndOtherInodes(t *testing.T) {
	locks := []byte(`1: FLOCK  ADVISORY  WRITE 3172515 103:02:28486201 0 EOF
1: -> FLOCK  ADVISORY  WRITE 3172896 103:02:28486201 0 EOF
1:  -> FLOCK  ADVISORY  WRITE 3173089 103:02:28486201 0 EOF
2: POSIX  ADVISORY  WRITE 812 00:1a:77 0 EOF
3: OFDLCK ADVISORY  READ  -1 00:05:28486201 0 EOF
4: FLOCK  ADVISORY  WRITE 900 103:02:128486201 0 EOF
5: FLOCK  ADVISORY  READ  901 fd:00:77 0 EOF
`)
	if got, want := locksOnInode(locks, 28486201), []lockHit{{"103:02", true}, {"00:05", false}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("inode 28486201: got %v want %v", got, want)
	}
	if got, want := locksOnInode(locks, 77), []lockHit{{"00:1a", false}, {"fd:00", true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("inode 77: got %v want %v", got, want)
	}
	if got := locksOnInode(locks, 5); got != nil {
		t.Fatalf("inode 5: got %v", got)
	}
}

func TestDevKeyMatchesProcLocks(t *testing.T) {
	for _, c := range []struct {
		dev  uint64
		want string
	}{
		{0x10302, "103:02"}, // nvme0n1p2: major 259, minor 2
		{0x0801, "08:01"},
		{0x001a, "00:1a"},
		{0x10000fd00, "fd:100000"}, // a large minor uses the high bits
	} {
		if got := devKey(c.dev); got != c.want {
			t.Errorf("devKey(%#x) = %s, want %s", c.dev, got, c.want)
		}
	}
}
