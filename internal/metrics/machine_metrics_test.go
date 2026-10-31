package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalFromFakeProc(t *testing.T) {
	root := t.TempDir()
	must := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("meminfo", "MemTotal:       32000000 kB\nMemFree:  100 kB\nMemAvailable:    8192000 kB\n")
	must("loadavg", "3.50 2.00 1.00 2/900 12345\n")
	m := Local(root)
	if !m.Known || m.FreeMB != 8000 || m.Load1 != 3.5 || m.Cores < 1 {
		t.Fatalf("got %+v", m)
	}
	if s := Score(m, 500, false); s != 8000-1750 {
		t.Fatalf("score = %d", s)
	}
	if s := Score(m, 500, true); s != 8000 {
		t.Fatalf("ram-only score = %d", s)
	}
}

func TestLocalUnknownWithoutProc(t *testing.T) {
	if m := Local(filepath.Join(t.TempDir(), "nope")); m.Known {
		t.Fatalf("missing /proc must be unknown, got %+v", m)
	}
}

func TestRealProc(t *testing.T) {
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		t.Skip("no /proc")
	}
	if m := Local("/proc"); !m.Known || m.FreeMB <= 0 {
		t.Fatalf("real /proc: %+v", m)
	}
}
