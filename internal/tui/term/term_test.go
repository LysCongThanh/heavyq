package term

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestSaneSize(t *testing.T) {
	tests := []struct {
		cols, rows int
		want       bool
	}{
		{80, 24, true}, {1, 1, true}, {1000, 500, true},
		{0, 0, false}, {0, 24, false}, {80, 0, false}, {-1, 5, false},
		{1001, 24, false}, {80, 501, false}, {65535, 65535, false},
	}
	for _, tc := range tests {
		if got := SaneSize(tc.cols, tc.rows); got != tc.want {
			t.Errorf("SaneSize(%d, %d) = %v, want %v", tc.cols, tc.rows, got, tc.want)
		}
	}
}

func TestDebouncerCollapsesABurstIntoOneCall(t *testing.T) {
	var calls atomic.Int32
	d := NewDebouncer(30*time.Millisecond, func() { calls.Add(1) })
	for i := 0; i < 10; i++ {
		d.Trigger()
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
	d.Trigger()
	d.Stop()
	time.Sleep(80 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("Stop did not cancel the pending call: %d", n)
	}
}

func TestStartRefusesAFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notatty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := Start(f, f); err != ErrNotTerminal {
		t.Fatalf("Start on a regular file = %v, want ErrNotTerminal", err)
	}
}
