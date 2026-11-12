// Package fleet is the observation layer of `heavyq fleet`: every machine (load, RAM, pressure, the
// processes that use them) and every queue (heavyq's own and the foreign ones of [[foreign]]), read-only.
// It never schedules and never writes anything but the report it is asked for.
//
// The rules (what is overloaded, what is an infrastructure failure, what worker cap to suggest) are pure
// functions with table tests; the I/O (history tail, ticket dirs, the probe script) is kept beside them.
package fleet

import (
	"fmt"
	"strings"
)

// Exit classes of a finished run.
const (
	ClassOK    = "ok"
	ClassInfra = "infra" // the machine or the queue failed: ssh dropped, killed, a tool missing, the queue's own codes
	ClassCode  = "code"  // the command ran and said no
)

// BuiltinInfraExits mean infrastructure for every queue: 127 (command not found), 137 (killed, 128+9:
// the OOM killer or kill -9) and 255 (the ssh connection dropped).
var BuiltinInfraExits = []int{127, 137, 255}

// ExitClassifier splits red runs into infra and code.
type ExitClassifier struct{ infra map[int]bool }

// NewClassifier is BuiltinInfraExits plus a queue's own infra exit codes.
func NewClassifier(extra []int) ExitClassifier {
	c := ExitClassifier{infra: map[int]bool{}}
	for _, e := range append(append([]int{}, BuiltinInfraExits...), extra...) {
		c.infra[e] = true
	}
	return c
}

// Class of an exit code.
func (c ExitClassifier) Class(exit int) string {
	switch {
	case exit == 0:
		return ClassOK
	case c.infra[exit]:
		return ClassInfra
	default:
		return ClassCode
	}
}

// The overload rule. A machine is OVERLOADED when any of these holds:
//   - load1 / cores >= OverloadLoadPerCore: half again as many runnable threads as cores, so every job runs
//     at two thirds of its speed or worse;
//   - free RAM (MemAvailable) < OverloadFreeShare of the total, or < OverloadFreeFloorMB when the total is
//     unknown: the next job swaps or meets the OOM killer;
//   - Linux memory pressure "full avg10" >= OverloadMemFullAvg10: all tasks stalled on memory that share
//     of the last 10 s.
//
// CPU pressure is shown but is not a trigger on its own: it is already in the load.
const (
	OverloadLoadPerCore  = 1.5
	OverloadFreeShare    = 0.10
	OverloadFreeFloorMB  = 1024
	OverloadMemFullAvg10 = 10.0
)

// Pressure is Linux pressure stall information (/proc/pressure), avg10 in percent.
type Pressure struct {
	CPUSomeAvg10 float64 `json:"cpu_some_avg10"`
	MemSomeAvg10 float64 `json:"mem_some_avg10"`
	MemFullAvg10 float64 `json:"mem_full_avg10"`
}

// Sample is what the overload rule reads of one machine.
type Sample struct {
	Cores           int
	Load1           float64
	FreeMB, TotalMB int
	Pressure        *Pressure
}

// Overload returns why the machine is overloaded; empty means it is not (or cannot be judged).
func Overload(s Sample) []string {
	var why []string
	if s.Cores > 0 {
		if r := s.Load1 / float64(s.Cores); r >= OverloadLoadPerCore {
			why = append(why, fmt.Sprintf("load/cores %.2f >= %.1f", r, OverloadLoadPerCore))
		}
	}
	if s.TotalMB > 0 && float64(s.FreeMB) < OverloadFreeShare*float64(s.TotalMB) {
		why = append(why, fmt.Sprintf("free RAM %d MB < %.0f%% of %d MB", s.FreeMB, OverloadFreeShare*100, s.TotalMB))
	} else if s.TotalMB <= 0 && s.Cores > 0 && s.FreeMB < OverloadFreeFloorMB {
		why = append(why, fmt.Sprintf("free RAM %d MB < %d MB", s.FreeMB, OverloadFreeFloorMB))
	}
	if s.Pressure != nil && s.Pressure.MemFullAvg10 >= OverloadMemFullAvg10 {
		why = append(why, fmt.Sprintf("memory pressure full %.1f%% >= %.0f%%", s.Pressure.MemFullAvg10, OverloadMemFullAvg10))
	}
	return why
}

// SlotCount is how many heavy slots one queue has on a machine.
type SlotCount struct {
	Owner string
	Heavy int
}

// WorkerCap is the suggested number of workers (test runner processes or threads) per heavy slot.
type WorkerCap struct {
	Workers    int    `json:"workers"`
	Cores      int    `json:"cores"`
	HeavySlots int    `json:"heavy_slots"`
	Why        string `json:"why"`
}

// SuggestCap divides the cores among every queue's heavy slots on the machine: when each of N concurrent
// heavy runs starts a runner with one worker per core, the machine carries N times its cores in runnable
// threads. cores / heavy slots (at least 1) keeps a full machine near load = cores.
func SuggestCap(cores int, slots []SlotCount) (WorkerCap, bool) {
	total := 0
	var parts []string
	for _, s := range slots {
		if s.Heavy > 0 {
			total += s.Heavy
			parts = append(parts, fmt.Sprintf("%s %d", s.Owner, s.Heavy))
		}
	}
	if cores < 1 || total == 0 {
		return WorkerCap{}, false
	}
	w := max(1, cores/total)
	return WorkerCap{Workers: w, Cores: cores, HeavySlots: total,
		Why: fmt.Sprintf("%d cores / %d heavy slots (%s) = %d workers per run", cores, total, strings.Join(parts, " + "), w)}, true
}
