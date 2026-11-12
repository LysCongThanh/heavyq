package metrics

import "testing"

// A stripped PATH (cron runs jobs with PATH=/usr/bin:/bin, where /usr/sbin's
// sysctl is not found) must not turn the RAM/load gate off: the local Mac is
// still measurable, the sampler just has to find its tools by absolute path.
func TestLocalMacOSMeasuresUnderStrippedPath(t *testing.T) {
	for _, path := range []string{"/usr/bin:/bin", t.TempDir()} {
		t.Setenv("PATH", path)
		m := localMacOS()
		if !m.Known || m.TotalMB <= 0 || m.Cores < 1 {
			t.Fatalf("PATH=%q: localMacOS() = %+v, want a known reading", path, m)
		}
	}
}
