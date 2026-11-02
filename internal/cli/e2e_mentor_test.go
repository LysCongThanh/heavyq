package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/mentor"
)

// mentorRig is a rig in a real git repository, so that runs have a tree and a
// run_id, with the mentor's inputs pinned: a fixed seed, and a time budget wide
// enough for a -race binary on a busy machine (HEAVYQ_TEST=1).
func mentorRig(t *testing.T, holdoutPct string) (*rig, string) {
	t.Helper()
	h := newRig(t, baseConfig+"[mentor]\nholdout_pct = "+holdoutPct+"\n")
	repo := gitRepo(t, h)
	h.env = append(h.env, "HEAVYQ_TEST=1", "HEAVYQ_MENTOR_SEED=e2e-seed")
	return h, repo
}

func postHook(cwd, command, event string) string {
	b, _ := json.Marshal(map[string]any{"session_id": "s-e2e", "cwd": cwd, "hook_event_name": event,
		"tool_name": "Bash", "tool_input": map[string]any{"command": command},
		"tool_response": map[string]any{"stdout": "SECRET-OUTPUT-NEVER-ECHOED"}})
	return string(b)
}

// failRun runs a command that always exits 3 through heavyq, as an agent
// retrying it would. The hook is told about it with the wrapped command below.
func (h *rig) failRun(t *testing.T) {
	t.Helper()
	if rc, out := h.run("4", "--", "sh", "-c", "exit 3"); rc != 3 {
		t.Fatalf("rc=%d %s", rc, out)
	}
}

const wrapped = "heavyq 4 -- sh -c 'exit 3'"

func (h *rig) contextOf(t *testing.T, out string) (event, text string) {
	t.Helper()
	var resp struct {
		H struct {
			Event string `json:"hookEventName"`
			Ctx   string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return resp.H.Event, resp.H.Ctx
}

func TestE2EMentorSecondFailureFactThirdDirective(t *testing.T) {
	h, repo := mentorRig(t, "0") // no holdout: every event is shown
	h.failRun(t)
	if rc, out, errOut := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse"); rc != 0 || out != "" || errOut != "" {
		t.Fatalf("the 1st failure is silent: rc=%d out=%q err=%q", rc, out, errOut)
	}
	h.failRun(t)
	rc, out, errOut := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse")
	if rc != 0 || errOut != "" {
		t.Fatalf("rc=%d stderr=%s", rc, errOut)
	}
	ev, text := h.contextOf(t, out)
	if ev != "PostToolUseFailure" || !strings.HasPrefix(text, mentor.Prefix) || !strings.Contains(text, "exited 3 twice in a row on the same tree") ||
		strings.Contains(text, "Next:") || strings.Contains(out, "SECRET-OUTPUT") || strings.Count(text, "\n") != 0 {
		t.Fatalf("2nd failure note: %s", out)
	}
	// The same record again (the agent ran `ls` in between): nothing, a state is never repeated aloud.
	if _, out, _ := h.runStdin(postHook(repo, "ls", "PostToolUse"), "hook", "claude-posttooluse"); out != "" {
		t.Fatalf("a repeated state was announced twice: %s", out)
	}
	time.Sleep(3200 * time.Millisecond) // the per-signature cooldown
	h.failRun(t)
	_, out, _ = h.runStdin(postHook(repo, wrapped+" 2>&1 | tail -30", "PostToolUse"), "hook", "claude-posttooluse") // a pipe into tail exits 0
	ev, text = h.contextOf(t, out)
	if ev != "PostToolUse" || !strings.Contains(text, "exited 3 3 times in a row") || !strings.Contains(text, "Next: change the code or its inputs") {
		t.Fatalf("3rd failure directive: %s", out)
	}
	if log := h.read("hook.log"); !strings.Contains(log, "mentor: repeat-failure level 2 arm inject lane app") {
		t.Fatalf("events are logged in hook.log:\n%s", log)
	}
}

func TestE2EMentorHoldoutArmIsSilentButLogged(t *testing.T) {
	h, repo := mentorRig(t, "100") // shadow mode: every event is held out
	h.failRun(t)
	h.failRun(t)
	rc, out, errOut := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse")
	if rc != 0 || out != "" || errOut != "" {
		t.Fatalf("holdout must print nothing: rc=%d out=%q err=%q", rc, out, errOut)
	}
	log := h.read("mentor/interventions.jsonl")
	runs := h.history()
	var e mentor.Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(log)), &e); err != nil {
		t.Fatalf("one log line expected: %v %q", err, log)
	}
	if e.Rule != mentor.RuleRepeatFailure || e.Arm != mentor.ArmHoldout || e.Seed != "e2e-seed" || e.Level != mentor.LevelFact ||
		e.Event != runs[1].ID || e.RunID != runs[1].RunID || e.Lane != "app" || e.Session != "s-e2e" || e.Outcome != mentor.OutcomePending {
		t.Fatalf("log entry: %+v (second run: %+v)", e, runs[1])
	}
}

func TestE2EMentorStatsOutcomeMuteAndEscapes(t *testing.T) {
	h, repo := mentorRig(t, "0")
	h.failRun(t)
	h.failRun(t)
	if _, out, _ := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse"); out == "" {
		t.Fatal("expected a note")
	}
	// The agent changes course: a different heavy run follows. stats settles it.
	if rc, out := h.run("4", "--", "sh", "-c", "exit 0"); rc != 0 {
		t.Fatalf("rc=%d %s", rc, out)
	}
	rc, out := h.run("mentor", "stats", "--json")
	var stats struct {
		Rules []mentor.RuleStats `json:"rules"`
	}
	if err := json.Unmarshal([]byte(out), &stats); rc != 0 || err != nil || len(stats.Rules) != 1 {
		t.Fatalf("stats --json: rc=%d %v\n%s", rc, err, out)
	}
	if r := stats.Rules[0]; r.Rule != mentor.RuleRepeatFailure || r.Inject.Events != 1 || r.Inject.Resolved != 1 || r.Inject.Differed != 1 || r.Holdout.Events != 0 {
		t.Fatalf("stats: %+v", r)
	}
	if rc, out := h.run("mentor", "stats"); rc != 0 || !strings.Contains(out, "repeat-failure") || !strings.Contains(out, "inject") || !strings.Contains(out, "n/a") {
		t.Fatalf("human stats: rc=%d\n%s", rc, out)
	}

	// Mute: a third failure on a fresh tree produces neither a note nor a log line.
	if rc, out := h.run("mentor", "mute", "repeat-failure"); rc != 0 || !strings.Contains(out, "muted repeat-failure") {
		t.Fatalf("mute: rc=%d %s", rc, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "edit.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.failRun(t)
	h.failRun(t)
	if _, out, _ := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse"); out != "" {
		t.Fatalf("a muted rule must be silent: %s", out)
	}
	if rc, out := h.run("mentor", "mute", "no-such-rule"); rc != ExitUsage || !strings.Contains(out, "unknown mentor rule") {
		t.Fatalf("unknown rule: rc=%d %s", rc, out)
	}
	if rc, _ := h.run("mentor", "unmute", "repeat-failure"); rc != 0 {
		t.Fatal("unmute failed")
	}

	// HEAVYQ_MENTOR=off silences it for a process tree.
	if err := os.WriteFile(filepath.Join(repo, "edit.txt"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.failRun(t)
	h.failRun(t)
	saved := h.env
	h.env = append(append([]string{}, h.env...), "HEAVYQ_MENTOR=off")
	if _, out, _ := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse"); out != "" {
		t.Fatalf("HEAVYQ_MENTOR=off must silence the hook: %s", out)
	}
	h.env = saved
	if _, out, _ := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse"); out == "" {
		t.Fatal("without the switch the same record is still news: off must not consume it")
	}
}

func TestE2EMentorHookFailsOpen(t *testing.T) {
	h, repo := mentorRig(t, "0")
	h.failRun(t)
	h.failRun(t)
	for name, in := range map[string]string{
		"garbage":      "{not json",
		"empty":        "",
		"another tool": `{"hook_event_name":"PostToolUse","tool_name":"Edit","cwd":"` + repo + `","tool_input":{}}`,
		"pre event":    postHook(repo, wrapped, "PreToolUse"),
		"unknown lane": postHook(h.root, "ls", "PostToolUse"),
	} {
		if rc, out, errOut := h.runStdin(in, "hook", "claude-posttooluse"); rc != 0 || out != "" || errOut != "" {
			t.Errorf("%s: rc=%d out=%q err=%q", name, rc, out, errOut)
		}
	}
	// A broken config: no note, exit 0, the problem in hook.log.
	if err := os.WriteFile(filepath.Join(h.root, "config.toml"), []byte("[mentor]\nholdout_pct = 500\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, out, _ := h.runStdin(postHook(repo, wrapped, "PostToolUseFailure"), "hook", "claude-posttooluse"); rc != 0 || out != "" {
		t.Fatalf("bad config: rc=%d out=%q", rc, out)
	}
	if !strings.Contains(h.read("hook.log"), "holdout_pct") {
		t.Fatalf("the config error must be logged:\n%s", h.read("hook.log"))
	}
}
