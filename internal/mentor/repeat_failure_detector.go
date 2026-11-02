package mentor

import (
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// Levels of the repeat-failure rule: the 2nd identical failure gets a fact,
// the 3rd (and later) a directive with a next action.
const (
	LevelFact      = 2
	LevelDirective = 3
)

// Signal is what a rule found: one repeat failure, in structured fields. The
// message is rendered from it (RenderMessage), never from raw output.
type Signal struct {
	Rule      string
	Signature string // run_id of the failing run: the identity state and cooldown are kept under
	Level     int    // LevelFact or LevelDirective
	Streak    int    // identical failures in a row on this tree
	Exit      int
	Cmd       string // the ledger's redacted command, as recorded
	Tree      string
	Event     string // id of the ledger record that raised the signal
	At        time.Time
}

// excludedExits are exit codes that say nothing about the code under test:
// 124 (timeout), 137 (SIGKILL, usually the OOM killer), 130 and 143 (SIGINT,
// SIGTERM: somebody stopped it), and heavyq's own 75-78 and 255 (queue and
// remote failures: the command did not run).
var excludedExits = map[int]bool{0: true, 124: true, 130: true, 137: true, 143: true, 75: true, 76: true, 77: true, 78: true, 255: true}

// ExcludedExit reports whether a failure with this exit code can never raise
// a repeat-failure signal.
func ExcludedExit(code int) bool { return excludedExits[code] }

// realRun reports whether r is a run the agent's command actually made: it ran
// to its end and is not a replay of another run's result.
func realRun(r history.Record) bool {
	return r.Result == "" && r.Cache != "hit" && r.Cache != "coalesced"
}

// DetectRepeatFailure looks at the records of one lane (oldest first) and
// reports a signal when the newest real run failed for the 2nd (or later)
// time, in a row, with the same exit code, on the same run_id (so the same
// tree, argv and environment).
//
// It stays silent when the failure is not deterministic evidence: the tree is
// unknown, the exit code is excluded (timeout, kill, heavyq's own codes), the
// machine was over its limits at start (under_load), or the same run ever
// passed on this tree in the window (flaky). Cache replays and runs that never
// ran (gave up, cancelled) are not counted.
func DetectRepeatFailure(recs []history.Record) (Signal, bool) {
	var real []history.Record
	for _, r := range recs {
		if realRun(r) {
			real = append(real, r)
		}
	}
	if len(real) == 0 {
		return Signal{}, false
	}
	last := real[len(real)-1]
	if last.RunID == "" || last.Tree == "" || ExcludedExit(last.Exit) || last.Loaded() {
		return Signal{}, false
	}
	for _, r := range real {
		if r.RunID == last.RunID && r.Exit == 0 {
			return Signal{}, false // it passed on this tree: flaky, not a repeat
		}
	}
	streak := 0
	for i := len(real) - 1; i >= 0; i-- {
		r := real[i]
		if r.RunID != last.RunID {
			continue // other commands in between do not break the repetition
		}
		if r.Exit != last.Exit || r.Loaded() {
			break
		}
		streak++
	}
	if streak < LevelFact {
		return Signal{}, false
	}
	level := LevelFact
	if streak >= LevelDirective {
		level = LevelDirective
	}
	return Signal{Rule: RuleRepeatFailure, Signature: last.RunID, Level: level, Streak: streak,
		Exit: last.Exit, Cmd: shortCommand(last.Cmd), Tree: last.Tree, Event: last.ID, At: last.End}, true
}

// shortCommand makes the ledger's command safe and short for a note: one
// line, no backticks (the note wraps it in them), at most 60 characters.
func shortCommand(cmd string) string {
	cmd = strings.Map(func(r rune) rune {
		if r == '`' || r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, cmd)
	cmd = strings.Join(strings.Fields(cmd), " ")
	if rs := []rune(cmd); len(rs) > 60 {
		cmd = string(rs[:59]) + "…"
	}
	return cmd
}
