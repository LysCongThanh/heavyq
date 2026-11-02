package steer

import (
	"strings"
	"testing"
)

// held is an item of lane "api" in the given state.
func held(id, state string) ItemFact {
	return ItemFact{ID: id, State: state, Lane: "api", Reason: "not run on this code yet: go test ./auth/..."}
}

func redItem(id string, cmd string) ItemFact {
	it := held(id, StateClaimed)
	it.Failing = []FailedCmd{{Cmd: cmd, Exit: 1, Class: "failed"}}
	return it
}

func facts(items ...ItemFact) Facts {
	return Facts{Lane: "api", Tree: "tree1", Items: items}
}

func baseline(cmds ...string) SessionState {
	return SessionState{HasBaseline: true, Baseline: cmds}
}

func TestDecideStopTable(t *testing.T) {
	const cmd = "go test ./auth/..."
	cases := []struct {
		name       string
		in         StopInput
		want       Action
		wantBlocks int    // State.Blocks after
		wantText   string // substring of Text (Block, Continue)
		wantNote   string // substring of Note
	}{
		{name: "off never blocks", in: StopInput{Event: EventStop, Mode: Off, Facts: facts(held("login", StateClaimed)), State: baseline()},
			want: Allow},
		{name: "guard blocks an unproven claimed item and names the exact next command",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(held("login", StateClaimed)), State: baseline()},
			want: Block, wantBlocks: 1, wantText: "Next: heavyq item done login --run"},
		{name: "the block offers the escapes",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(held("login", StateClaimed)), State: baseline()},
			want: Block, wantBlocks: 1, wantText: `heavyq item block login "needs: <what>" or heavyq item release login`},
		{name: "proven item does not block",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(held("login", StateProven)), State: baseline()},
			want: Allow},
		{name: "blocked item (with needs) does not block",
			in: StopInput{Event: EventStop, Mode: Guard, State: baseline(),
				Facts: facts(ItemFact{ID: "login", State: StateBlocked, Lane: "api", Needs: "design sign-off"})},
			want: Allow},
		{name: "released item is open and does not block",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(ItemFact{ID: "login", State: StateOpen}), State: baseline()},
			want: Allow},
		{name: "another lane's item does not block",
			in: StopInput{Event: EventStop, Mode: Guard, State: baseline(),
				Facts: facts(ItemFact{ID: "login", State: StateClaimed, Lane: "web"})},
			want: Allow},
		{name: "an item without a declared proof cannot be proven, so it does not block",
			in: StopInput{Event: EventStop, Mode: Guard, State: baseline(),
				Facts: facts(ItemFact{ID: "login", State: StateClaimed, Lane: "api", NoProof: true})},
			want: Allow},
		{name: "ticked by hand but unproven blocks",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(held("login", StateDoneUnproven)), State: baseline()},
			want: Block, wantBlocks: 1, wantText: "login"},
		{name: "stop_hook_active on the same situation: allowed (no progress)",
			in: StopInput{Event: EventStop, Mode: Guard, StopHookActive: true, Facts: facts(held("login", StateClaimed)),
				State: SessionState{HasBaseline: true, Blocks: 1, LastSig: signature(facts(held("login", StateClaimed)), blockers(StopInput{Mode: Guard, Facts: facts(held("login", StateClaimed))}))}},
			want: Allow, wantBlocks: 1},
		{name: "stop_hook_active but the tree changed since the last block: blocks again",
			in: StopInput{Event: EventStop, Mode: Guard, StopHookActive: true, Facts: facts(held("login", StateClaimed)),
				State: SessionState{HasBaseline: true, Blocks: 1, LastSig: "another-tree#login|x"}},
			want: Block, wantBlocks: 2},
		{name: "guard cap: 3 blocks already, the agent may stop, with a note",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(held("login", StateClaimed)), State: SessionState{HasBaseline: true, Blocks: 3}},
			want: Allow, wantBlocks: 3, wantNote: "released after 3 blocks"},
		{name: "guard: the third block is still a block",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(held("login", StateClaimed)), State: SessionState{HasBaseline: true, Blocks: 2}},
			want: Block, wantBlocks: 3, wantText: "Stop blocks left: 0"},
		{name: "destroy has no cap",
			in:   StopInput{Event: EventStop, Mode: Destroy, Facts: facts(held("login", StateClaimed)), State: SessionState{HasBaseline: true, Blocks: 40}},
			want: Block, wantBlocks: 41, wantText: "destroy: no cap"},
		{name: "destroy ignores stop_hook_active",
			in: StopInput{Event: EventStop, Mode: Destroy, StopHookActive: true, Facts: facts(held("login", StateClaimed)),
				State: SessionState{HasBaseline: true, Blocks: 1, LastSig: signature(facts(held("login", StateClaimed)), blockers(StopInput{Mode: Destroy, Facts: facts(held("login", StateClaimed))}))}},
			want: Block, wantBlocks: 2},
		{name: "destroy re-injects the brief on every block",
			in:   StopInput{Event: EventStop, Mode: Destroy, Facts: Facts{Goal: "ship login", Lane: "api", Items: []ItemFact{held("login", StateClaimed)}}},
			want: Block, wantBlocks: 1, wantText: "goal: ship login"},
		{name: "guard does not gate SubagentStop", in: StopInput{Event: EventSubagentStop, Mode: Guard, Facts: facts(held("login", StateClaimed)), State: baseline()},
			want: Allow},
		{name: "destroy gates SubagentStop", in: StopInput{Event: EventSubagentStop, Mode: Destroy, Facts: facts(held("login", StateClaimed)), State: baseline()},
			want: Block, wantBlocks: 1},
		{name: "guard does not gate TaskCompleted", in: StopInput{Event: EventTaskCompleted, Mode: Guard, Facts: facts(held("login", StateClaimed)), State: baseline()},
			want: Allow},
		{name: "destroy gates TaskCompleted", in: StopInput{Event: EventTaskCompleted, Mode: Destroy, Facts: facts(held("login", StateClaimed)), State: baseline()},
			want: Block, wantBlocks: 1},
		{name: "destroy gates TeammateIdle", in: StopInput{Event: EventTeammateIdle, Mode: Destroy, Facts: facts(held("login", StateClaimed)), State: baseline()},
			want: Block, wantBlocks: 1},
		{name: "baseline red: the proof was already failing at session start, never blocks",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(redItem("login", cmd)), State: baseline(cmd)},
			want: Allow},
		{name: "red now but green at start: blocks and says what failed",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(redItem("login", cmd)), State: baseline()},
			want: Block, wantBlocks: 1, wantText: "failed on this code: go test ./auth/... (exit 1)"},
		{name: "red and no baseline was recorded: whose fault is unknown, so allow",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(redItem("login", cmd)), State: SessionState{}},
			want: Allow},
		{name: "unproven (not red) and no baseline: still blocks",
			in:   StopInput{Event: EventStop, Mode: Guard, Facts: facts(held("login", StateClaimed)), State: SessionState{}},
			want: Block, wantBlocks: 1},
		{name: "tree changed since the last passing gate while an item is held and proven",
			in: StopInput{Event: EventStop, Mode: Guard, State: baseline(),
				Facts: Facts{Lane: "api", Tree: "t2", Items: []ItemFact{held("login", StateProven)}, Gate: &GateFact{Name: "ready", Older: true}}},
			want: Block, wantBlocks: 1, wantText: "Next: heavyq gate run ready"},
		{name: "gate stale but no item held: nothing to block on",
			in: StopInput{Event: EventStop, Mode: Guard, State: baseline(),
				Facts: Facts{Lane: "api", Items: []ItemFact{{ID: "login", State: StateOpen}}, Gate: &GateFact{Name: "ready", Older: true}}},
			want: Allow},
		{name: "gate red at baseline never blocks via the stale-gate rule",
			in: StopInput{Event: EventStop, Mode: Guard, State: baseline(cmd),
				Facts: Facts{Lane: "api", Items: []ItemFact{held("login", StateProven)}, Gate: &GateFact{Name: "ready", Older: true, Failing: []FailedCmd{{Cmd: cmd, Exit: 1, Class: "failed"}}}}},
			want: Allow},
		{name: "claim vs ledger: says tests pass, the gate failed on this tree: one continuation",
			in: StopInput{Event: EventStop, Mode: Guard, LastMessage: "Done. All tests pass.", State: baseline(),
				Facts: Facts{Lane: "api", Gate: &GateFact{Name: "ready", Failing: []FailedCmd{{Cmd: cmd, Exit: 1, Class: "failed"}}}}},
			want: Continue, wantText: "ledger shows `go test ./auth/...` failed on this tree (exit 1)"},
		{name: "claim vs ledger is used once per session",
			in: StopInput{Event: EventStop, Mode: Guard, LastMessage: "All tests pass.", State: SessionState{HasBaseline: true, ClaimNudged: true},
				Facts: Facts{Lane: "api", Gate: &GateFact{Name: "ready", Failing: []FailedCmd{{Cmd: cmd, Exit: 1, Class: "failed"}}}}},
			want: Allow},
		{name: "claim vs ledger never continues while stop_hook_active",
			in: StopInput{Event: EventStop, Mode: Guard, StopHookActive: true, LastMessage: "All tests pass.", State: baseline(),
				Facts: Facts{Lane: "api", Gate: &GateFact{Name: "ready", Failing: []FailedCmd{{Cmd: cmd, Exit: 1, Class: "failed"}}}}},
			want: Allow},
		{name: "claim vs ledger ignores a failure that was red at baseline",
			in: StopInput{Event: EventStop, Mode: Guard, LastMessage: "All tests pass.", State: baseline(cmd),
				Facts: Facts{Lane: "api", Gate: &GateFact{Name: "ready", Failing: []FailedCmd{{Cmd: cmd, Exit: 1, Class: "failed"}}}}},
			want: Allow},
		{name: "claim vs ledger ignores flaky and under_load failures",
			in: StopInput{Event: EventStop, Mode: Guard, LastMessage: "All tests pass.", State: baseline(),
				Facts: Facts{Lane: "api", Gate: &GateFact{Name: "ready", Failing: []FailedCmd{{Cmd: cmd, Exit: 1, Class: "flaky"}, {Cmd: "x", Exit: 1, Class: "under_load"}}}}},
			want: Allow},
		{name: "claim without a contradiction: nothing to say",
			in: StopInput{Event: EventStop, Mode: Guard, LastMessage: "All tests pass.", State: baseline(),
				Facts: Facts{Lane: "api", Gate: &GateFact{Name: "ready", Proven: true}}},
			want: Allow},
		{name: "claim vs ledger works for SubagentStop",
			in: StopInput{Event: EventSubagentStop, Mode: Guard, LastMessage: "All tests pass.", State: baseline(),
				Facts: Facts{Lane: "api", Gate: &GateFact{Name: "ready", Failing: []FailedCmd{{Cmd: cmd, Exit: 1, Class: "failed"}}}}},
			want: Continue},
		{name: "claim vs ledger is off when steering is off",
			in: StopInput{Event: EventStop, Mode: Off, LastMessage: "All tests pass.", State: baseline(),
				Facts: Facts{Lane: "api", Gate: &GateFact{Name: "ready", Failing: []FailedCmd{{Cmd: cmd, Exit: 1, Class: "failed"}}}}},
			want: Allow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := DecideStop(c.in)
			if d.Action != c.want {
				t.Fatalf("action = %v, want %v (why: %s, text: %s)", d.Action, c.want, d.Why, d.Text)
			}
			if d.State.Blocks != c.wantBlocks {
				t.Errorf("blocks = %d, want %d", d.State.Blocks, c.wantBlocks)
			}
			if !strings.Contains(d.Text, c.wantText) {
				t.Errorf("text = %q, want it to contain %q", d.Text, c.wantText)
			}
			if !strings.Contains(d.Note, c.wantNote) {
				t.Errorf("note = %q, want it to contain %q", d.Note, c.wantNote)
			}
			if d.Action == Continue && !d.State.ClaimNudged {
				t.Error("a claim continuation must be recorded as used")
			}
			if d.Action == Block && !strings.HasPrefix(d.Text, prefix) {
				t.Errorf("a block must start with %q: %q", prefix, d.Text)
			}
		})
	}
}

// The same facts that blocked once must not block forever: after the cap, and on every later stop.
func TestGuardNeverBlocksMoreThanTheCap(t *testing.T) {
	f := facts(held("login", StateClaimed))
	st := baseline()
	blocks := 0
	for i := 0; i < 20; i++ {
		d := DecideStop(StopInput{Event: EventStop, Mode: Guard, Facts: f, State: st})
		if d.Action == Block {
			blocks++
		}
		st = d.State
	}
	if blocks != GuardBlockCap {
		t.Fatalf("guard blocked %d times, want %d", blocks, GuardBlockCap)
	}
}

func TestBlockTextForSeveralItemsListsThemAndNamesTheFirstNext(t *testing.T) {
	f := facts(held("a", StateClaimed), held("b", StateClaimed), held("c", StateClaimed), held("d", StateClaimed))
	d := DecideStop(StopInput{Event: EventStop, Mode: Guard, Facts: f, State: baseline()})
	if d.Action != Block || !strings.Contains(d.Text, "4 items") || !strings.Contains(d.Text, "+1 more") || !strings.Contains(d.Text, "Next: heavyq item done a --run") {
		t.Fatalf("%v %q", d.Action, d.Text)
	}
	if len(d.Text) > 900 {
		t.Fatalf("a guard block is %d bytes, want a short message", len(d.Text))
	}
}

func TestParseEvent(t *testing.T) {
	for _, s := range []string{"Stop", "SubagentStop", "TaskCompleted", "TeammateIdle"} {
		if e, ok := ParseEvent(s); !ok || string(e) != s {
			t.Errorf("%s: %v %v", s, e, ok)
		}
	}
	for _, s := range []string{"", "stop", "PreToolUse", "Stop "} {
		if _, ok := ParseEvent(s); ok {
			t.Errorf("%q accepted", s)
		}
	}
}
