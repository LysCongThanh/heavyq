package fleet

import (
	"errors"
	"strings"
)

// probeSteps are the probe script's steps in the order it runs them, cheapest first, with what a report
// lacks when a step did not finish (each later step needs the earlier ones, except wt).
var probeSteps = []struct{ name, missing string }{
	{"base", "the machine's numbers"},
	{"slots", "slot states, top consumers and attribution"},
	{"procs", "top consumers and attribution"},
	{"args", "command lines of the busiest"},
	{"wt", "heavyq remote job attribution"},
}

// stepState is what the script said about its steps.
type stepState struct {
	done    map[string]bool
	skipped map[string]string // step -> seconds the script had run when it gave up on it
	ended   bool              // the "end" line arrived: the script ran to its end
}

// stepLine reads "done <step>", "skip <step> <seconds>" and "end"; false for any other line.
func (s *stepState) stepLine(f []string) bool {
	switch {
	case len(f) == 1 && f[0] == "end":
		s.ended = true
	case len(f) == 2 && f[0] == "done":
		s.done[f[1]] = true
	case len(f) == 3 && f[0] == "skip":
		s.skipped[f[1]] = f[2]
	default:
		return false
	}
	return true
}

// settle keeps the facts of the steps that finished and drops the rest (half a process table would
// attribute wrongly; half the slot files would count wrongly), and sets Partial to what is missing and why.
// cut is why the transport ended before the script did ("" when it ran to its end); wantWT is whether a
// work dir was asked, so whether the script had a wt step to do.
func (p *Probe) settle(s stepState, cut string, wantWT bool) error {
	if !s.done["base"] {
		return errors.New("the probe answered without the machine's numbers")
	}
	if !s.done["slots"] {
		p.Slots = nil
	}
	if !s.done["procs"] {
		p.Procs, p.ticks, p.clk = nil, map[int]float64{}, 0
	}
	if !s.done["args"] {
		p.Args = map[int]string{}
	}
	if !s.done["wt"] {
		p.InWorkTree = map[int]bool{}
	}
	var budget, timeout []string
	for _, st := range probeSteps[1:] {
		if s.done[st.name] {
			continue // a step that finished just as its budget ran out is whole
		}
		if secs, ok := s.skipped[st.name]; ok {
			budget = append(budget, st.missing+" (the probe's time budget was spent after "+secs+"s)")
			if st.name == "procs" {
				break // the script stops there: its "top consumers and attribution" covers the later steps
			}
			continue
		}
		if s.ended || s.done[st.name] || (st.name == "wt" && !wantWT) {
			continue
		}
		why := cut
		if why == "" {
			why = "the probe ended early"
		}
		timeout = append(timeout, st.missing+" ("+why+")")
		break // a cut leaves every later step undone too; the first one says what is missing
	}
	if missing := append(budget, timeout...); len(missing) > 0 {
		p.Partial = "no " + strings.Join(missing, "; no ")
	}
	return nil
}
