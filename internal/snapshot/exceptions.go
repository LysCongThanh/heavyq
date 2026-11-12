package snapshot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

const (
	// LoopWindow is how far back repeated failures count as a loop.
	LoopWindow = 30 * time.Minute
	// GiveUpWindow is how far back a give-up marks a lane as starved.
	GiveUpWindow = time.Hour
	// outsideRAMShare: processes outside the queue holding more than this
	// share of the machine's RAM are an exception.
	outsideRAMShare = 0.4
	// maxExceptions bounds the list, so a bad day does not bury the page.
	maxExceptions = 20
)

// ComputeExceptions lists what needs a human, most urgent first: red gates,
// loops, blocked items, starved lanes, machine trouble. It is a pure function of the
// snapshot (with its Gates filled in), the ledger (oldest first) and now.
// Identical exceptions (same ID) appear once.
func ComputeExceptions(s Snapshot, recs []history.Record, now time.Time) []Exception {
	var all []Exception
	all = append(all, redGates(s)...)
	all = append(all, loops(s, recs, now)...)
	all = append(all, blockedItems(s)...)
	all = append(all, starved(s, recs, now)...)
	all = append(all, machineTrouble(s)...)
	seen := map[string]bool{}
	out := make([]Exception, 0, len(all))
	for _, e := range all {
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	rank := map[string]int{KindRedGate: 0, KindLoop: 1, KindBlocked: 2, KindStarved: 3, KindMachine: 4}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Severity == SeverityCritical) != (b.Severity == SeverityCritical) {
			return a.Severity == SeverityCritical
		}
		if rank[a.Kind] != rank[b.Kind] {
			return rank[a.Kind] < rank[b.Kind]
		}
		return a.ID < b.ID
	})
	if len(out) > maxExceptions {
		out = out[:maxExceptions]
	}
	return out
}

// redGates: a lane's gate has a command whose latest conclusive run on the
// lane's current tree failed, and the failure is not classed as noise.
func redGates(s Snapshot) []Exception {
	var out []Exception
	for _, g := range s.Gates {
		if g.State != GateFailed {
			continue
		}
		var real []GateFailure
		for _, f := range g.Failed {
			if f.Class != ClassFlaky && f.Class != ClassUnderLoad {
				real = append(real, f)
			}
		}
		if len(real) == 0 {
			continue
		}
		f := real[0]
		for _, x := range real[1:] {
			if x.At.After(f.At) {
				f = x
			}
		}
		text := fmt.Sprintf("%s's %s gate fails on its current code: %s failed", g.Lane, g.Gate, quote(f.Cmd))
		if len(real) > 1 {
			text += fmt.Sprintf(", and %d more commands with it", len(real)-1)
		}
		cmd := "heavyq gate run " + g.Gate
		if g.Worktree != "" {
			cmd = "cd " + shellQuote(g.Worktree) + " && " + cmd
		}
		out = append(out, Exception{
			ID: strings.Join([]string{KindRedGate, g.Lane, g.Tree}, "|"), Kind: KindRedGate, Severity: SeverityCritical,
			Lane: g.Lane, Text: text + ".",
			Evidence: Evidence{At: f.At, Tree: g.Tree, Cmd: f.Cmd, Count: f.Count, Gate: g.Gate},
			Action:   &Action{Kind: ActionRun, Target: g.Gate, Command: cmd},
		})
	}
	return out
}

// machineTrouble: unreachable, paused, or local RAM taken by processes
// outside the queue.
func machineTrouble(s Snapshot) []Exception {
	var out []Exception
	for _, m := range s.Machines {
		e := Exception{Kind: KindMachine, Severity: SeverityWarning, Machine: m.Name}
		switch {
		case !m.Reachable:
			e.Text, e.Evidence.Detail = m.Name+" is unreachable, so nothing can start there.", "unreachable"
		case m.Paused:
			e.Text, e.Evidence.Detail = m.Name+" is paused: no new job starts there.", "paused"
			e.Action = &Action{Kind: ActionResume, Target: m.Name, Command: "heavyq resume " + shellQuote(m.Name)}
		default:
			continue
		}
		e.ID = KindMachine + "|" + m.Name + "|" + e.Evidence.Detail
		out = append(out, e)
	}
	out = append(out, untrustedExits(s)...)
	if e, ok := outsideRAM(s); ok {
		out = append(out, e)
	}
	return out
}

// outsideRAM: processes outside the queue hold more than outsideRAMShare of
// the local machine's RAM.
func outsideRAM(s Snapshot) (Exception, bool) {
	o := s.CPU.OutsideTotal
	for _, m := range s.Machines {
		if m.Name != "local" || !s.CPU.Available || m.TotalMB <= 0 || float64(o.RSSMB) <= outsideRAMShare*float64(m.TotalMB) {
			continue
		}
		who := ""
		if len(s.CPU.Outside) > 0 {
			top := s.CPU.Outside[0]
			who = fmt.Sprintf(", led by %s (%d processes)", top.Name, top.Procs)
			if top.Procs == 1 {
				who = fmt.Sprintf(", led by %s", top.Name)
			}
		}
		return Exception{
			ID: KindMachine + "|" + m.Name + "|outside_ram", Kind: KindMachine, Severity: SeverityWarning, Machine: m.Name,
			Text:     fmt.Sprintf("Processes outside the queue hold %s of the %s of RAM on %s%s.", mb(o.RSSMB), mb(m.TotalMB), m.Name, who),
			Evidence: Evidence{Detail: fmt.Sprintf("outside the queue: %s of %s (%d%%)", mb(o.RSSMB), mb(m.TotalMB), 100*o.RSSMB/m.TotalMB)},
		}, true
	}
	return Exception{}, false
}

// quote puts a command in backticks, shortened to one line.
func quote(cmd string) string {
	if i := strings.Index(cmd, " heavyq-gate "); i >= 0 { // a gate command runs through a wrapper script
		cmd = cmd[i+len(" heavyq-gate "):]
	}
	cmd = strings.TrimPrefix(cmd, "sh -c ")
	if r := []rune(cmd); len(r) > 60 {
		cmd = string(r[:59]) + "…"
	}
	return "`" + cmd + "`"
}

// shellQuote makes s safe to paste into a shell as one word.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./:@+=,") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// untrustedExits: remotes whose exit canary failed. Their runs prove nothing, so a gate placed there cannot pass.
func untrustedExits(s Snapshot) []Exception {
	var out []Exception
	for _, m := range s.Machines {
		if m.ExitUntrusted == "" {
			continue
		}
		e := Exception{Kind: KindMachine, Severity: SeverityWarning, Machine: m.Name,
			Text:     UntrustedExitText(m.Name, m.ExitUntrusted) + "; its runs prove nothing until the canary passes again.",
			Evidence: Evidence{Detail: "exit-untrusted"}}
		e.ID = KindMachine + "|" + m.Name + "|" + e.Evidence.Detail
		out = append(out, e)
	}
	return out
}

// UntrustedExitText is the one phrase every surface uses for a remote that failed the exit canary.
func UntrustedExitText(machine, reason string) string {
	return "exit codes not trustworthy on " + machine + ": " + reason
}
