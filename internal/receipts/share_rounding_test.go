package receipts

import "testing"

func TestApproxCount(t *testing.T) {
	for n, want := range map[int]string{
		0: "0", 1: "1", 9: "9", 10: "~10", 12: "~10", 13: "~15", 38: "~40", 42: "~40", 43: "~45", 99: "~100",
		100: "~100", 124: "~100", 126: "~150", 430: "~450", 999: "~1000", 1234: "~1000", 1260: "~1500", 12345: "~10000",
	} {
		if got := approxCount(n); got != want {
			t.Errorf("approxCount(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestApproxRate(t *testing.T) {
	for f, want := range map[float64]string{1: "~1", 1.4: "~1", 2.6: "~3", 9.4: "~9", 41.7: "~40", 57: "~55", 410: "~400"} {
		if got := approxRate(f); got != want {
			t.Errorf("approxRate(%v) = %s, want %s", f, got, want)
		}
	}
}

func TestRatioRuns(t *testing.T) {
	for _, c := range []struct {
		part, total int
		want        string
	}{
		{0, 100, ""}, {5, 0, ""}, {1, 1000, "fewer than 1 in 100 runs"}, {1, 151, "fewer than 1 in 100 runs"}, {1, 150, "about 1 in 100 runs"},
		{1, 100, "about 1 in 100 runs"}, {5, 100, "about 1 in 20 runs"}, {12, 100, "about 1 in 8 runs"},
		{13, 100, "about 1 in 8 runs"}, {40, 100, "about half of runs"}, {1, 7, "about 1 in 6 runs"}, {1, 3, "about 1 in 3 runs"},
		{1, 2, "about half of runs"}, {3, 5, "most runs"}, {9, 10, "nearly all runs"}, {10, 10, "nearly all runs"},
	} {
		if got := ratioRuns(c.part, c.total); got != c.want {
			t.Errorf("ratioRuns(%d, %d) = %q, want %q", c.part, c.total, got, c.want)
		}
	}
}

func TestApproxDuration(t *testing.T) {
	for s, want := range map[float64]string{
		0: "under 1s", 0.9: "under 1s", 1: "~1s", 3.4: "~3s", 9.6: "~10s", 12: "~10s", 14: "~15s", 52: "~50s", 54.9: "~55s",
		55: "~1m", 89: "~1m", 90: "~2m", 130: "~2m", 599: "~10m", 600: "~10m", 700: "~10m", 800: "~15m", 3000: "~50m",
		3300: "~1h", 3600: "~1h", 5400: "~1.5h", 7300: "~2h", 36000: "~10h", 90000: "~25h",
	} {
		if got := approxDuration(s); got != want {
			t.Errorf("approxDuration(%v) = %s, want %s", s, got, want)
		}
	}
}
