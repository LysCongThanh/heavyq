package snapshot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/pools"
)

// maxCauses bounds how many resource causes the first sentence names.
const maxCauses = 2

// queueSentence is the headline: nothing happens, only jobs run, or who
// waits and why.
func queueSentence(s Snapshot, head *Waiting) Sentence {
	w, r := len(s.Waiting), len(s.Running)
	b := &sentenceBuilder{}
	switch {
	case w == 0 && r == 0:
		b.em("All quiet.").text(" Nothing runs, nothing waits.")
	case w == 0:
		b.em(plural(r, "job")).text(" running and ").em("nobody waits").text(".")
	default:
		verb := "wait"
		if w == 1 {
			verb = "waits"
		}
		who := plural(w, "agent") + " " + verb
		reasons := sortedReasons(head.Reasons)
		if startsNow(reasons) {
			b.em(who).text(", and ").lane(head.Lane).text(" starts on the next poll.")
			break
		}
		var causes [][]Span
		for _, mr := range reasons {
			if !isBehind(mr.reason) && len(causes) < maxCauses {
				causes = append(causes, causeOf(mr.machine, mr.reason))
			}
		}
		b.em(who).text(" because ")
		if len(causes) == 0 {
			b.text("the queue ahead is full")
		}
		for i, c := range causes {
			if i > 0 {
				b.text(" and ")
			}
			b.spans(c...)
		}
		b.text(".")
	}
	return *b.build()
}

// causeOf turns one machine's wait reason (the strings of schedule.Gate and
// the slot claim) into a clause about that machine.
func causeOf(machine, reason string) []Span {
	b := &sentenceBuilder{}
	switch short, isRAM := ramShort(reason); {
	case isRAM:
		b.em(machine).text(" is ").num(mb(short)).text(" short on RAM")
	case pools.IsReason(reason):
		b.text("pool ").em(strings.TrimPrefix(reason, pools.ReasonPrefix))
	case strings.Contains(reason, "slots busy"):
		b.text("every heavy slot on ").em(machine).text(" is taken")
	case strings.HasPrefix(reason, "load"):
		b.em(machine).text(" is over its load limit")
	case strings.Contains(reason, "unreachable"):
		b.em(machine).text(" is unreachable")
	case strings.Contains(reason, "paused"):
		b.em(machine).text(" is paused")
	case reason == OtherConfigReason:
		b.text("its own config decides for ").em(machine)
	default:
		b.text(machine + ": " + reason)
	}
	return b.parts
}

// minOutsideShare is the share of local RAM that processes outside the queue
// must hold before the story mentions them.
const minOutsideShare = 0.3

// outsideSentence names what eats the machine outside the queue.
func outsideSentence(s Snapshot) *Sentence {
	local := machineNamed(s, "local")
	if !s.CPU.Available || local == nil || local.TotalMB <= 0 || len(s.CPU.Outside) == 0 {
		return nil
	}
	total, top := s.CPU.OutsideTotal, s.CPU.Outside[0]
	if float64(total.RSSMB)/float64(local.TotalMB) <= minOutsideShare {
		return nil
	}
	noun, verb := "processes", "hold"
	if top.Procs == 1 {
		noun, verb = "process", "holds"
	}
	b := &sentenceBuilder{}
	b.text("Outside the queue, ").em(fmt.Sprintf("%d %s %s", top.Procs, top.Name, noun)).text(" " + verb + " ").num(mb(top.RSSMB))
	if len(s.CPU.Outside) > 1 {
		b.text(", everything else ").num(mb(total.RSSMB - top.RSSMB))
	}
	return b.text(".").build()
}

// bypassSentence tells when the head ticket skips the load check
// (anti-starvation; RAM is always checked), or that it already does.
func bypassSentence(s Snapshot, head *Waiting, elapsed float64) *Sentence {
	if head == nil || head.BypassInS < 0 || !s.Config.ResourceGate || !allBlocked(head.Reasons) {
		return nil
	}
	b := &sentenceBuilder{}
	if left := head.BypassInS - elapsed; left > 0 {
		b.lane(head.Lane).text(" skips the load check in ").num(dur(left)).text(".")
	} else {
		b.lane(head.Lane).text(" now skips the load check.")
	}
	return b.build()
}

// forcedSentence names the lanes that got in through the bypass.
func forcedSentence(s Snapshot) *Sentence {
	var lanes []string
	seen := map[string]bool{}
	for _, t := range s.Running {
		if t.Forced && !seen[t.Lane] {
			seen[t.Lane] = true
			lanes = append(lanes, t.Lane)
		}
	}
	if len(lanes) == 0 {
		return nil
	}
	b := &sentenceBuilder{}
	for i, l := range lanes {
		if i > 0 {
			b.text(", ")
		}
		b.lane(l)
	}
	return b.text(" got in through that bypass.").build()
}

// unreachableSentence flags machines that do not answer.
func unreachableSentence(s Snapshot) *Sentence {
	var down []string
	for _, m := range s.Machines {
		if !m.Reachable {
			down = append(down, m.Name)
		}
	}
	if len(down) == 0 {
		return nil
	}
	verb := "is"
	if len(down) > 1 {
		verb = "are"
	}
	return (&sentenceBuilder{}).hot(strings.Join(down, ", ") + " " + verb + " unreachable.").build()
}

// windowSentence summarises the insights window, when the caller has one.
func windowSentence(ins *Insights) *Sentence {
	if ins == nil || ins.Runs+ins.GaveUp+ins.CacheHits+ins.Coalesced == 0 {
		return nil
	}
	return (&sentenceBuilder{}).
		text("In the last " + windowLabel(ins.To.Sub(ins.From)) + ": ").
		num(plural(ins.Runs, "run")).text(", wait p95 ").num(dur(ins.WaitP95S)).text(", ").
		num(strconv.Itoa(ins.GaveUp)).text(" gave up, ").
		num(strconv.Itoa(ins.CacheHits + ins.Coalesced)).text(" didn't need to run.").build()
}

// machineNamed returns the machine called name, or nil.
func machineNamed(s Snapshot, name string) *Machine {
	for i := range s.Machines {
		if s.Machines[i].Name == name {
			return &s.Machines[i]
		}
	}
	return nil
}
