package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scenario the issue asks for: claim an item, edit, Stop is blocked with the next command; the gate runs, Stop is allowed.
func TestE2ESteerGuardBlocksUntilTheGateProvesTheItem(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	if out := h.mustRun(app, 0, "steer", "guard"); !strings.Contains(out, "steer: guard") {
		t.Fatalf("steer guard: %s", out)
	}
	rc, out, errOut := h.hookIn(app, nil, sessionStartJSON(app, "s1", "startup", ""), "hook", "claude-sessionstart")
	if rc != 0 || errOut != "" {
		t.Fatalf("sessionstart rc=%d %s", rc, errOut)
	}
	if ev, text := additionalContextOf(t, out); ev != "SessionStart" || !strings.Contains(text, "goal: ship login v2") ||
		!strings.Contains(text, "next: heavyq item claim --next") {
		t.Fatalf("brief: %s", text)
	}
	h.mustRun(app, 0, "item", "claim", "login-api")
	if err := os.WriteFile(filepath.Join(app, "feature.txt"), []byte("work in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stop := func(active bool) (int, string, string) {
		return h.hookIn(app, nil, stopJSON(app, "s1", "Stop", active, "I implemented login."), "hook", "claude-stop")
	}
	rc, out, errOut = stop(false)
	if rc != 0 || errOut != "" {
		t.Fatalf("stop rc=%d %s", rc, errOut)
	}
	reason := blockReasonOf(t, out)
	if !strings.Contains(reason, "Next: heavyq item done login-api --run") || !strings.Contains(reason, "heavyq item block login-api") ||
		!strings.Contains(reason, "Stop blocks left: 2") {
		t.Fatalf("a block must name the next command and the escapes:\n%s", reason)
	}
	// Claude Code is now continuing because of the block: nothing changed, so the agent may stop (stop_hook_active).
	if rc, out, _ := stop(true); rc != 0 || out != "" {
		t.Fatalf("stop_hook_active on an unchanged tree must allow: rc=%d %q", rc, out)
	}

	if out := h.mustRun(app, 0, "gate", "run"); !strings.Contains(out, "PASS") && !strings.Contains(out, "REUSED") {
		t.Logf("gate run output:\n%s", out)
	}
	if rc, out, errOut := stop(false); rc != 0 || out != "" || errOut != "" {
		t.Fatalf("the item is proven on this tree: Stop must be allowed, rc=%d out=%q err=%q", rc, out, errOut)
	}
	if st := h.read("hook.log"); !strings.Contains(st, "blocked: login-api") {
		t.Fatalf("the block was not logged:\n%s", st)
	}
}

func TestE2ESteerOffIsTheDefaultAndTheKillSwitchWins(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	h.mustRun(app, 0, "item", "claim", "login-api")
	in := stopJSON(app, "s1", "Stop", false, "done")
	// Nothing stored: off.
	if rc, out, errOut := h.hookIn(app, nil, in, "hook", "claude-stop"); rc != 0 || out != "" || errOut != "" {
		t.Fatalf("default must be off: rc=%d %q %q", rc, out, errOut)
	}
	h.mustRun(app, 0, "steer", "destroy")
	if rc, out, _ := h.hookIn(app, nil, in, "hook", "claude-stop"); rc != 0 || !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("destroy must block: rc=%d %q", rc, out)
	}
	if rc, out, _ := h.hookIn(app, []string{"HEAVYQ_STEER=off"}, in, "hook", "claude-stop"); rc != 0 || out != "" {
		t.Fatalf("HEAVYQ_STEER=off is the kill switch: rc=%d %q", rc, out)
	}
	if rc, out, _ := h.hookIn(app, []string{"HEAVYQ_STEER=banana"}, in, "hook", "claude-stop"); rc != 0 || out != "" {
		t.Fatalf("an unknown override must kill, not guess: rc=%d %q", rc, out)
	}
	if out := h.mustRun(app, 0, "steer"); !strings.Contains(out, "destroy") {
		t.Fatalf("steer shows the mode: %s", out)
	}
	if _, o, _ := h.hookIn(app, []string{"HEAVYQ_STEER=off"}, "", "steer"); !strings.Contains(o, "HEAVYQ_STEER overrides") {
		t.Fatalf("steer must say the override: %q", o)
	}
}

// A tree that was already red when the session started is "pre-existing": never a reason to block.
func TestE2ESteerBaselineRedNeverBlocks(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	if err := os.WriteFile(filepath.Join(app, "red.flag"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, _, _ := h.inDir(app, nil, "gate", "run"); rc == 0 {
		t.Fatal("the gate must be red while red.flag exists")
	}
	h.mustRun(app, 0, "steer", "guard")
	rc, out, _ := h.hookIn(app, nil, sessionStartJSON(app, "s2", "startup", ""), "hook", "claude-sessionstart")
	if _, text := additionalContextOf(t, out); rc != 0 || !strings.Contains(text, "red before this session started: not yours") {
		t.Fatalf("the brief must say the red gate is not the agent's: %s", text)
	}
	h.mustRun(app, 0, "item", "claim", "login-api")
	if rc, out, _ := h.hookIn(app, nil, stopJSON(app, "s2", "Stop", false, "done"), "hook", "claude-stop"); rc != 0 || out != "" {
		t.Fatalf("a red that predates the session must not block: rc=%d %q", rc, out)
	}
	// Another session that started on the same red tree after compaction has no baseline of its own: do not guess.
	h.mustRun(app, 0, "item", "release", "login-api")
	h.mustRun(app, 0, "item", "claim", "login-api")
	h.hookIn(app, nil, sessionStartJSON(app, "s3", "compact", ""), "hook", "claude-sessionstart")
	if rc, out, _ := h.hookIn(app, nil, stopJSON(app, "s3", "Stop", false, "done"), "hook", "claude-stop"); rc != 0 || out != "" {
		t.Fatalf("a red item with no recorded baseline must not block: rc=%d %q", rc, out)
	}
}

// Claim vs ledger: non-blocking, and once.
func TestE2ESteerClaimContradictedByTheLedgerContinuesOnce(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	h.mustRun(app, 0, "steer", "guard")
	h.hookIn(app, nil, sessionStartJSON(app, "s4", "startup", ""), "hook", "claude-sessionstart")
	if err := os.WriteFile(filepath.Join(app, "red.flag"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, _, _ := h.inDir(app, nil, "gate", "run"); rc == 0 {
		t.Fatal("the gate must be red")
	}
	say := func(msg string) string {
		rc, out, errOut := h.hookIn(app, nil, stopJSON(app, "s4", "Stop", false, msg), "hook", "claude-stop")
		if rc != 0 || errOut != "" {
			t.Fatalf("rc=%d %s", rc, errOut)
		}
		return out
	}
	if out := say("Refactored. Everything looks fine."); out != "" {
		t.Fatalf("no claim, no continuation: %q", out)
	}
	ev, text := additionalContextOf(t, say("Done. All tests pass."))
	if ev != "Stop" || !strings.Contains(text, "ledger shows `test ! -f red.flag` failed on this tree (exit 1)") || !strings.Contains(text, "Next: heavyq gate run ready") {
		t.Fatalf("continuation: %s", text)
	}
	if out := say("Done. All tests pass."); out != "" {
		t.Fatalf("the continuation happens once per session: %q", out)
	}
}

func TestE2ESteerDestroyGatesTaskCompletedWithExit2AndGuardDoesNot(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	h.mustRun(app, 0, "item", "claim", "login-api")
	in := jsonString(map[string]any{"session_id": "s5", "cwd": app, "hook_event_name": "TaskCompleted", "task_id": "t1", "task_subject": "login"})

	h.mustRun(app, 0, "steer", "guard")
	if rc, out, errOut := h.hookIn(app, nil, in, "hook", "claude-stop"); rc != 0 || out != "" || errOut != "" {
		t.Fatalf("guard must not gate TaskCompleted: rc=%d %q %q", rc, out, errOut)
	}
	h.mustRun(app, 0, "steer", "destroy")
	rc, out, errOut := h.hookIn(app, nil, in, "hook", "claude-stop")
	if rc != 2 || out != "" || !strings.Contains(errOut, "Next: heavyq item done login-api --run") {
		t.Fatalf("destroy must block TaskCompleted with exit 2 and the reason on stderr: rc=%d out=%q err=%q", rc, out, errOut)
	}
	// The event argument decides, whatever the input says.
	plain := jsonString(map[string]any{"session_id": "s5", "cwd": app, "hook_event_name": "Stop", "stop_hook_active": false})
	if rc, _, errOut := h.hookIn(app, nil, plain, "hook", "claude-stop", "TeammateIdle"); rc != 2 || !strings.Contains(errOut, "login-api") {
		t.Fatalf("event arg TeammateIdle: rc=%d %q", rc, errOut)
	}
	// SubagentStop blocks with JSON like Stop.
	if rc, out, _ := h.hookIn(app, nil, stopJSON(app, "s5", "SubagentStop", false, ""), "hook", "claude-stop"); rc != 0 || !strings.Contains(blockReasonOf(t, out), "login-api") {
		t.Fatalf("SubagentStop in destroy: rc=%d %q", rc, out)
	}
	// Destroy has no cap: many blocks in a row, still blocking, with the brief re-injected.
	var last string
	for i := 0; i < 5; i++ {
		_, o, _ := h.hookIn(app, nil, stopJSON(app, "s5", "Stop", i > 0, "x"), "hook", "claude-stop")
		last = blockReasonOf(t, o)
	}
	if !strings.Contains(last, "goal: ship login v2") {
		t.Fatalf("destroy re-injects the brief on every block:\n%s", last)
	}
}

func TestE2ESteerHooksFailOpen(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	h.mustRun(app, 0, "steer", "destroy")
	h.mustRun(app, 0, "item", "claim", "login-api")
	for _, args := range [][]string{{"hook", "claude-stop"}, {"hook", "claude-sessionstart"}, {"hook", "claude-stop", "Bogus"}, {"hook", "claude-stop", "Stop", "extra"}} {
		for _, in := range []string{"{not json", "", "[1]", `{"cwd":"/definitely/not/a/repo","session_id":"s","hook_event_name":"Stop"}`, `{"session_id":"s"}`} {
			if rc, out, errOut := h.hookIn(app, nil, in, args...); rc != 0 || out != "" || errOut != "" {
				t.Errorf("%v with %q: must say nothing and exit 0: rc=%d %q %q", args, in, rc, out, errOut)
			}
		}
	}
	// No session id: nothing can be counted, so nothing is blocked.
	if rc, out, _ := h.hookIn(app, nil, jsonString(map[string]any{"cwd": app, "hook_event_name": "Stop"}), "hook", "claude-stop"); rc != 0 || out != "" {
		t.Fatalf("no session id: rc=%d %q", rc, out)
	}
	// A broken config must never block an agent.
	if err := os.WriteFile(filepath.Join(h.root, "config.toml"), []byte("[hook]\npermission = \"maybe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, out, _ := h.hookIn(app, nil, stopJSON(app, "s6", "Stop", false, "x"), "hook", "claude-stop"); rc != 0 || out != "" {
		t.Fatalf("broken config: rc=%d %q", rc, out)
	}
}
