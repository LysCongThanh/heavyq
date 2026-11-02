package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

// The rig turns the automatic flow refresh off (HEAVYQ_FLOW=off) so the other end-to-end tests do not pay for it;
// the flow tests turn it on for the hook or the run they are about.
var flowOn = []string{"HEAVYQ_FLOW=on"}

func permissionRequestJSON(cwd, session, command string) string {
	return jsonString(map[string]any{"session_id": session, "cwd": cwd, "hook_event_name": "PermissionRequest",
		"tool_name": "Bash", "tool_input": map[string]any{"command": command, "description": "run it"}})
}

func notificationJSON(cwd, session, kind, message string) string {
	return jsonString(map[string]any{"session_id": session, "cwd": cwd, "hook_event_name": "Notification",
		"notification_type": kind, "message": message})
}

func postToolUseJSON(cwd, session string) string {
	return jsonString(map[string]any{"session_id": session, "cwd": cwd, "hook_event_name": "PostToolUse",
		"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}})
}

// flowSnapshot is `flow show --json` of dir.
func (h *rig) flowSnapshot(dir string) flow.Snapshot {
	h.t.Helper()
	out := h.mustRun(dir, 0, "flow", "show", "--json")
	var s flow.Snapshot
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		h.t.Fatalf("flow show --json: %v\n%s", err, out)
	}
	return s
}

// The acceptance test of the foundation: a PermissionRequest hook event makes `flow show --json` list the lane in
// needs_you, within that one event, with the reason and the session; the hook itself never decides anything.
func TestE2EPermissionRequestShowsNeedsYouWithinOneEvent(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	rc, out, errOut := h.hookIn(app, flowOn, permissionRequestJSON(app, "sess-1", "npm run e2e"), "hook", "claude-flow")
	if rc != 0 || out != "" || errOut != "" {
		t.Fatalf("an observer exits 0 and says nothing (never a permission decision): rc=%d out=%q err=%q", rc, out, errOut)
	}
	s := h.flowSnapshot(app)
	if len(s.NeedsYou) != 1 {
		t.Fatalf("needs_you after one event: %+v", s.NeedsYou)
	}
	n := s.NeedsYou[0]
	if n.Lane != "app" || n.Session != "sess-1" || n.Reason != "permission_request: Bash(npm run e2e)" || n.Since.IsZero() {
		t.Fatalf("%+v", n)
	}
	if len(s.Lanes) != 1 || s.Lanes[0].Lane != "app" || s.Lanes[0].State != flow.BlockedHuman {
		t.Errorf("the lane reads blocked_human: %+v", s.Lanes)
	}
	if s.Schema != 1 || s.Goal != "ship login v2" || s.Items.Total != 2 || s.Items.Open != 2 || string(s.Run) != "null" {
		t.Errorf("contract fields: schema=%d goal=%q items=%+v run=%s", s.Schema, s.Goal, s.Items, s.Run)
	}
	text := h.mustRun(app, 0, "flow", "show")
	if !strings.Contains(text, "needs you (1)") || !strings.Contains(text, "permission_request: Bash(npm run e2e)") {
		t.Errorf("text form:\n%s", text)
	}
}

// The Notification that follows the PermissionRequest about six seconds later does not add a second entry, and the
// stronger reason stays; the lane's next tool call (PostToolUse) answers it.
func TestE2ENotificationsMergeAndPostToolUseClears(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	h.hookIn(app, flowOn, permissionRequestJSON(app, "sess-1", "npm run e2e"), "hook", "claude-flow")
	h.hookIn(app, flowOn, notificationJSON(app, "sess-1", "permission_prompt", "Claude needs your permission to use Bash"), "hook", "claude-flow")
	s := h.flowSnapshot(app)
	if len(s.NeedsYou) != 1 || !strings.HasPrefix(s.NeedsYou[0].Reason, "permission_request:") {
		t.Fatalf("%+v", s.NeedsYou)
	}
	// Notifications that need nobody leave no trace.
	h.hookIn(app, flowOn, notificationJSON(app, "sess-2", "auth_success", "logged in"), "hook", "claude-flow")
	if s := h.flowSnapshot(app); len(s.NeedsYou) != 1 {
		t.Fatalf("auth_success needs no one: %+v", s.NeedsYou)
	}
	rc, out, _ := h.hookIn(app, flowOn, postToolUseJSON(app, "sess-1"), "hook", "claude-flow")
	if rc != 0 || out != "" {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
	if s := h.flowSnapshot(app); len(s.NeedsYou) != 0 {
		t.Fatalf("the lane's next tool call clears it: %+v", s.NeedsYou)
	}
	// idle_prompt and agent_needs_input count too.
	h.hookIn(app, flowOn, notificationJSON(app, "sess-1", "idle_prompt", "Claude is waiting for your input"), "hook", "claude-flow")
	if s := h.flowSnapshot(app); len(s.NeedsYou) != 1 || !strings.HasPrefix(s.NeedsYou[0].Reason, "idle_prompt:") {
		t.Fatalf("%+v", s.NeedsYou)
	}
}

// Command lines in a reason go through the redactor, and the log of events never holds the secret either.
func TestE2EFlowReasonIsRedacted(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	h.hookIn(app, flowOn, permissionRequestJSON(app, "sess-1", "API_TOKEN=hunter2hunter2 npm run deploy"), "hook", "claude-flow")
	s := h.flowSnapshot(app)
	if len(s.NeedsYou) != 1 || strings.Contains(s.NeedsYou[0].Reason, "hunter2") || !strings.Contains(s.NeedsYou[0].Reason, "API_TOKEN=***") {
		t.Fatalf("%+v", s.NeedsYou)
	}
	common := gitOutput(t, h, app, "rev-parse", "--path-format=absolute", "--git-common-dir")
	b, err := os.ReadFile(filepath.Join(common, "heavyq", "flow", "events.jsonl"))
	if err != nil || strings.Contains(string(b), "hunter2") {
		t.Fatalf("events log: %v\n%s", err, b)
	}
}

// StopFailure is an event the state shows: the turn ended on an API error, and the lane waits for someone.
func TestE2EStopFailureAppearsInTheState(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	in := jsonString(map[string]any{"session_id": "sess-1", "cwd": app, "hook_event_name": "StopFailure", "error": "rate_limit"})
	if rc, out, _ := h.hookIn(app, flowOn, in, "hook", "claude-flow"); rc != 0 || out != "" {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
	s := h.flowSnapshot(app)
	if len(s.Events) != 1 || s.Events[0].Kind != flow.EventStopFailure || s.Events[0].Detail != "rate_limit" || s.Events[0].Lane != "app" {
		t.Fatalf("events: %+v", s.Events)
	}
	if len(s.NeedsYou) != 1 || s.NeedsYou[0].Reason != "stop_failure: rate_limit" {
		t.Fatalf("needs_you: %+v", s.NeedsYou)
	}
}

// Hooks fail open: a repository outside git, bad JSON, an unknown event and a disabled flow all exit 0 and write nothing.
func TestE2EFlowHookFailsOpen(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	for name, c := range map[string]struct {
		dir, stdin string
		env        []string
	}{
		"not a repository": {h.root, permissionRequestJSON(h.root, "s", "ls"), flowOn},
		"bad json":         {app, "{nope", flowOn},
		"unknown event":    {app, jsonString(map[string]any{"session_id": "s", "cwd": app, "hook_event_name": "Elicitation"}), flowOn},
		"switched off":     {app, permissionRequestJSON(app, "s", "ls"), []string{"HEAVYQ_FLOW=off"}},
		"no cwd":           {app, jsonString(map[string]any{"session_id": "s", "hook_event_name": "PermissionRequest"}), flowOn},
	} {
		if rc, out, errOut := h.hookIn(c.dir, c.env, c.stdin, "hook", "claude-flow"); rc != 0 || out != "" || errOut != "" {
			t.Errorf("%s: rc=%d out=%q err=%q", name, rc, out, errOut)
		}
	}
	if s := h.flowSnapshot(app); len(s.NeedsYou) != 0 {
		t.Fatalf("nothing should have been recorded: %+v", s.NeedsYou)
	}
}

// The end of any run brings the state up to date, and `flow refresh` forces it.
func TestE2EFlowRefreshesAtTheEndOfARunAndOnDemand(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	common := gitOutput(t, h, app, "rev-parse", "--path-format=absolute", "--git-common-dir")
	state := filepath.Join(common, "heavyq", "flow", "state.json")
	if _, err := os.Stat(state); err == nil {
		t.Fatal("state before any event")
	}
	if rc, out, errOut := h.inDir(app, flowOn, "4", "--lane", "app", "--session", "sess-1", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d %s%s", rc, out, errOut)
	}
	b, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("a finished run writes the state: %v", err)
	}
	var s flow.Snapshot
	if err := json.Unmarshal(b, &s); err != nil || s.Items.Total != 2 || !strings.HasSuffix(s.Repo, "app") {
		t.Fatalf("%v %+v", err, s)
	}
	first := s.Updated
	// A second run inside the debounce window leaves the file alone; refresh forces it.
	if rc, _, _ := h.inDir(app, flowOn, "4", "--lane", "app", "--", "true"); rc != 0 {
		t.Fatal("run failed")
	}
	if s2 := h.flowSnapshot(app); !s2.Updated.Equal(first) {
		t.Fatalf("a run inside the 60 s debounce must not rewrite the state: %v -> %v", first, s2.Updated)
	}
	h.mustRun(app, 0, "flow", "refresh")
	if s3 := h.flowSnapshot(app); !s3.Updated.After(first) {
		t.Fatalf("flow refresh must write at once: %v -> %v", first, s3.Updated)
	}
	// With the automatic refresh off, a run writes nothing.
	plain := gitRepoAt(t, h, "plain")
	if rc, _, _ := h.inDir(plain, []string{"HEAVYQ_FLOW=off"}, "4", "--", "true"); rc != 0 {
		t.Fatal("run failed")
	}
	pc := gitOutput(t, h, plain, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if _, err := os.Stat(filepath.Join(pc, "heavyq", "flow", "state.json")); err == nil {
		t.Fatal("HEAVYQ_FLOW=off must stop the automatic write")
	}
}

// SessionStart and Stop (the brief's and the stop gate's hooks) refresh the state after their reply; the reply itself
// is unchanged.
func TestE2ESessionStartRefreshesTheFlowState(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	rc, out, _ := h.hookIn(app, flowOn, sessionStartJSON(app, "s1", "startup", ""), "hook", "claude-sessionstart")
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if _, text := additionalContextOf(t, out); !strings.Contains(text, "ship login v2") {
		t.Fatalf("the brief is unchanged:\n%s", text)
	}
	common := gitOutput(t, h, app, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if _, err := os.Stat(filepath.Join(common, "heavyq", "flow", "state.json")); err != nil {
		t.Fatalf("SessionStart must refresh the state: %v", err)
	}
}

// The status-line hook keeps the session's model and agent version, and the ledger joins them: agent and model are
// on the record of a run in that session. A model switch is a change point the state shows.
func TestE2ESessionMetaFeedsTheLedger(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	line := jsonString(map[string]any{"session_id": "sess-1", "cwd": app, "version": "2.1.90", "model": map[string]any{"id": "claude-opus-5"},
		"rate_limits": map[string]any{"five_hour": map[string]any{"used_percentage": 61.0}, "seven_day": map[string]any{"used_percentage": 20.0}}})
	if rc, out, errOut := h.hookIn(app, nil, line, "hook", "claude-sessionmeta"); rc != 0 || out != "" || errOut != "" {
		t.Fatalf("the status-line hook prints nothing: rc=%d out=%q err=%q", rc, out, errOut)
	}
	if rc, out := h.run("4", "--session", "sess-1", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Agent != "claude-code/2.1.90" || r.Model != "claude-opus-5" {
		t.Fatalf("agent and model on the record: %+v", r)
	}
	if rc, _ := h.run("4", "--", "true"); rc != 0 {
		t.Fatal("run failed")
	}
	if r := h.lastRecord(); r.Agent != "" || r.Model != "" {
		t.Fatalf("a run with no session has no agent: %+v", r)
	}

	sw := jsonString(map[string]any{"session_id": "sess-1", "cwd": app, "hook_event_name": "PostModelSwitch", "from_model": "claude-opus-5", "to_model": "claude-sonnet-5"})
	if rc, out, _ := h.hookIn(app, flowOn, sw, "hook", "claude-flow"); rc != 0 || out != "" {
		t.Fatalf("rc=%d out=%q", rc, out)
	}
	if rc, out := h.run("4", "--session", "sess-1", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if r := h.lastRecord(); r.Model != "claude-sonnet-5" {
		t.Fatalf("after the switch: %+v", r)
	}
	s := h.flowSnapshot(app)
	if len(s.Events) != 1 || s.Events[0].Kind != flow.EventModelSwitch || s.Events[0].Detail != "claude-sonnet-5" {
		t.Fatalf("events: %+v", s.Events)
	}
	if s.Session == nil || s.Session.Model != "claude-sonnet-5" || s.Session.AgentVersion != "2.1.90" ||
		s.Session.RateLimits == nil || *s.Session.RateLimits.FiveHourPct != 61 || *s.Session.RateLimits.SevenDayPct != 20 {
		t.Fatalf("session block: %+v", s.Session)
	}
}

// `hook install claude --features flow` registers the five events with the one observer command.
func TestE2EHookInstallFlowFeature(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	h.env = append(h.env, "CLAUDE_CONFIG_DIR="+filepath.Join(h.root, "claude-home"))
	h.mustRun(repo, 0, "hook", "install", "claude", "--features", "queue,flow")
	b, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"PermissionRequest"`, `"Notification"`, `"StopFailure"`, `"PostModelSwitch"`, `"PostToolUse"`,
		"hook claude-flow", "permission_prompt|idle_prompt|agent_needs_input"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings lack %s:\n%s", want, b)
		}
	}
}
