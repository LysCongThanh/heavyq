package receipts

import (
	"fmt"
	"math"
)

// roundStep rounds n (>= 1) to a coarse step that follows its size: whole
// numbers below 10, then half of the leading power of ten (5s up to 99, 50s
// up to 999, 500s up to 9,999, ...). 42 becomes 40, 38 becomes 40, 430 becomes 450.
func roundStep(n float64) int {
	step := 1.0
	if n >= 10 {
		step = math.Pow(10, math.Floor(math.Log10(n))) / 2
	}
	return int(math.Floor(n/step+0.5) * step)
}

// approxCount writes a count for the share card: exact below 10, else
// rounded and marked with "~".
func approxCount(n int) string {
	if n < 10 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("~%d", roundStep(float64(n)))
}

// approxRate writes a per-day rate (>= 1) as "~N".
func approxRate(f float64) string { return fmt.Sprintf("~%d", roundStep(f)) }

// oneInLadder are the denominators "about 1 in N" may take.
var oneInLadder = []int{2, 3, 4, 5, 6, 8, 10, 12, 15, 20, 30, 50, 100}

// ratioRuns says in words how many of total runs were part, as a noun phrase
// that ends in "runs": "about 1 in 8 runs", "most runs". "" when part is 0.
func ratioRuns(part, total int) string {
	if part <= 0 || total <= 0 {
		return ""
	}
	p := float64(part) / float64(total)
	switch {
	case p >= 0.9:
		return "nearly all runs"
	case p >= 0.6:
		return "most runs"
	case p >= 0.4:
		return "about half of runs"
	}
	inv := 1 / p
	if inv > 150 {
		return "fewer than 1 in 100 runs"
	}
	best := oneInLadder[0]
	for _, d := range oneInLadder {
		if math.Abs(float64(d)-inv) < math.Abs(float64(best)-inv) {
			best = d
		}
	}
	return fmt.Sprintf("about 1 in %d runs", best)
}

// approxDuration writes a wait in coarse human units: whole seconds below
// 10 s, 5 s steps below a minute, minutes below 10 min, 5 min steps below an
// hour, then half hours (whole hours from 10 h).
func approxDuration(s float64) string {
	switch {
	case s < 1:
		return "under 1s"
	case s < 10:
		return fmt.Sprintf("~%ds", int(math.Floor(s+0.5)))
	case s < 55:
		return fmt.Sprintf("~%ds", int(math.Floor(s/5+0.5))*5)
	case s < 600:
		return fmt.Sprintf("~%dm", max(1, int(math.Floor(s/60+0.5))))
	case s < 3300:
		return fmt.Sprintf("~%dm", int(math.Floor(s/300+0.5))*5)
	}
	h := s / 3600
	if h < 10 {
		return fmt.Sprintf("~%gh", math.Floor(h*2+0.5)/2)
	}
	return fmt.Sprintf("~%dh", int(math.Floor(h+0.5)))
}
