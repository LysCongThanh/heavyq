package metrics

import "testing"

func TestParseFake(t *testing.T) {
	m, ok := parseFake("load1=0.1, free_mb=99999,cores=8", 2)
	if !ok || !m.Known || m.Load1 != 0.1 || m.FreeMB != 99999 || m.Cores != 8 {
		t.Fatalf("got %+v ok=%v", m, ok)
	}
	if m, ok = parseFake("load1=9", 4); !ok || m.Cores != 4 || m.FreeMB != 0 {
		t.Fatalf("missing keys keep defaults: %+v ok=%v", m, ok)
	}
	for _, bad := range []string{"", "load1", "load1=x", "load1=-1", "cores=0", "free_mb=-5", "nope=1", "load1=1,,cores=2", "cores=8;load1=1"} {
		if m, ok := parseFake(bad, 4); ok {
			t.Errorf("%q must be rejected, got %+v", bad, m)
		}
	}
}

func TestHostHonorsTheOverrideOnlyInTestMode(t *testing.T) {
	const spec = "load1=7.5,free_mb=123,total_mb=456,cores=3"
	t.Setenv(envFakeMetrics, spec)

	for _, mode := range []string{"", "0", "true"} {
		t.Setenv(envTestMode, mode)
		if m := Host(t.TempDir()); m.Load1 == 7.5 || m.FreeMB == 123 {
			t.Fatalf("override applied with %s=%q: %+v", envTestMode, mode, m)
		}
	}

	t.Setenv(envTestMode, "1")
	m := Host(t.TempDir())
	if !m.Known || m.Load1 != 7.5 || m.FreeMB != 123 || m.TotalMB != 456 || m.Cores != 3 {
		t.Fatalf("got %+v", m)
	}

	t.Setenv(envFakeMetrics, "load1=oops")
	if m := Host(t.TempDir()); m.Load1 == 7.5 {
		t.Fatalf("malformed spec must be ignored: %+v", m)
	}
}
