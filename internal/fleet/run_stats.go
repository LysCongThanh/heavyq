package fleet

import (
	"math"
	"slices"
	"time"
)

// Run is one finished run of any queue, as the window statistics see it.
type Run struct {
	Queue   string    `json:"queue"`
	Machine string    `json:"machine"`
	Lane    string    `json:"lane"`
	Prio    int       `json:"prio"`
	Light   bool      `json:"light"`
	End     time.Time `json:"end"`
	WaitS   float64   `json:"wait_s"`
	RunS    float64   `json:"run_s"`
	Exit    int       `json:"exit"`
	Class   string    `json:"class"` // ClassOK, ClassInfra or ClassCode
}

// Stats summarise the runs of a window.
type Stats struct {
	Runs     int     `json:"runs"`
	RunHours float64 `json:"run_hours"`
	WaitP50S float64 `json:"wait_p50_s"`
	WaitP90S float64 `json:"wait_p90_s"`
	WaitP95S float64 `json:"wait_p95_s"`
	RunP50S  float64 `json:"run_p50_s"`
	PerHour  float64 `json:"per_hour"` // runs finished per hour of the window
	Red      int     `json:"red"`
	RedInfra int     `json:"red_infra"`
	RedCode  int     `json:"red_code"`
	// InfraRate and CodeRate are the shares of all runs that were red for each reason.
	InfraRate float64 `json:"infra_rate"`
	CodeRate  float64 `json:"code_rate"`
}

// Summarize computes Stats over runs that finished within a window of the given length.
func Summarize(runs []Run, window time.Duration) Stats {
	s := Stats{Runs: len(runs)}
	if len(runs) == 0 {
		return s
	}
	waits := make([]float64, 0, len(runs))
	lens := make([]float64, 0, len(runs))
	var runSecs float64
	for _, r := range runs {
		waits = append(waits, r.WaitS)
		lens = append(lens, r.RunS)
		runSecs += r.RunS
		switch r.Class {
		case ClassInfra:
			s.RedInfra++
		case ClassCode:
			s.RedCode++
		}
	}
	slices.Sort(waits)
	slices.Sort(lens)
	s.Red = s.RedInfra + s.RedCode
	s.RunHours = round2(runSecs / 3600)
	s.WaitP50S, s.WaitP90S, s.WaitP95S = Percentile(waits, 50), Percentile(waits, 90), Percentile(waits, 95)
	s.RunP50S = Percentile(lens, 50)
	if h := window.Hours(); h > 0 {
		s.PerHour = round2(float64(s.Runs) / h)
	}
	s.InfraRate = round2(float64(s.RedInfra) / float64(s.Runs))
	s.CodeRate = round2(float64(s.RedCode) / float64(s.Runs))
	return s
}

// Percentile is the nearest-rank percentile p (0-100] of sorted values; 0 for none.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(len(sorted)) / 100)) // p*n first: 0.9*20 is not exactly 18
	return sorted[min(max(rank, 1), len(sorted))-1]
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
