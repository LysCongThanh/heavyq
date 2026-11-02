package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

func stopFailureJSON(cwd, session, errWord string) string {
	return jsonString(map[string]any{"session_id": session, "cwd": cwd, "hook_event_name": "StopFailure", "error": errWord})
}

func laneEnv(lane string, more ...string) []string {
	return append(append([]string{}, flowOn...), append([]string{"HEAVYQ_LANE=" + lane}, more...)...)
}

// Tonight's sequence (2026-11-02, issue #120): four agents hit the usage limit within minutes of each other, the lead
// resumed them two hours later. The state must show ONE fleet entry while they were stopped, and nothing once any
// session showed activity again, not four lanes that stay blocked_human.
func TestE2EUsageLimitIsOneFleetEntryAndClearsOnTheNextActivity(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	lanes := []struct{ lane, session string }{{"heavyq", "s0"}, {"agent-a", "s1"}, {"agent-b", "s2"}, {"agent-c", "s3"}}
	for _, l := range lanes {
		if rc, out, errOut := h.hookIn(app, laneEnv(l.lane), stopFailureJSON(app, l.session, "rate_limit"), "hook", "claude-flow"); rc != 0 || out != "" || errOut != "" {
			t.Fatalf("%s: rc=%d out=%q err=%q", l.lane, rc, out, errOut)
		}
	}
	s := h.flowSnapshot(app)
	if len(s.NeedsYou) != 1 || s.NeedsYou[0].Lane != "fleet" || !strings.Contains(s.NeedsYou[0].Reason, "4 agents stopped") {
		t.Fatalf("one fleet entry for four stopped agents, got %+v", s.NeedsYou)
	}
	if s.Quota == nil || s.Quota.Agents != 4 || len(s.Quota.Lanes) != 4 {
		t.Fatalf("quota: %+v", s.Quota)
	}
	for _, l := range s.Lanes {
		if l.State == flow.BlockedHuman {
			t.Errorf("a usage limit is no person to wait on: %+v", l)
		}
	}
	if out := h.mustRun(app, 0, "flow", "show"); !strings.Contains(out, "quota hit at") || strings.Contains(out, "stop_failure: rate_limit  for") {
		t.Errorf("flow show must show the fleet entry, not a line per lane:\n%s", out)
	}

	// An agent finishing its last tool call right after the stops is not the quota coming back.
	if rc, _, _ := h.hookIn(app, laneEnv("agent-a"), postToolUseJSON(app, "s1"), "hook", "claude-flow"); rc != 0 {
		t.Fatal("hook failed")
	}
	if s := h.flowSnapshot(app); s.Quota == nil {
		t.Fatal("activity inside the grace must not lift the quota")
	}

	// The lead resumes: the next tool call of ANY session (here a fifth, unrelated one) lifts it for everyone.
	zero := laneEnv("agent-d", quotaGraceEnv+"=0s")
	if rc, _, _ := h.hookIn(app, zero, postToolUseJSON(app, "s9"), "hook", "claude-flow"); rc != 0 {
		t.Fatal("hook failed")
	}
	s = h.flowSnapshot(app)
	if s.Quota != nil || len(s.NeedsYou) != 0 {
		t.Fatalf("after activity: quota %+v, needs %+v", s.Quota, s.NeedsYou)
	}
	if out := h.mustRun(app, 0, "flow", "show"); !strings.Contains(out, "needs you: nothing") {
		t.Errorf("flow show after the resume:\n%s", out)
	}
}

// A second stop after the resume is a new episode, with its own start.
func TestE2EUsageLimitAfterAResumeIsANewEpisode(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	env := laneEnv("agent-a", quotaGraceEnv+"=0s")
	h.hookIn(app, env, stopFailureJSON(app, "s1", "rate_limit"), "hook", "claude-flow")
	h.hookIn(app, env, postToolUseJSON(app, "s9"), "hook", "claude-flow")
	if s := h.flowSnapshot(app); s.Quota != nil {
		t.Fatalf("lifted: %+v", s.Quota)
	}
	h.hookIn(app, env, stopFailureJSON(app, "s2", "rate_limit"), "hook", "claude-flow")
	if s := h.flowSnapshot(app); s.Quota == nil || s.Quota.Agents != 1 {
		t.Fatalf("a new stop opens a new episode of one agent: %+v", s.Quota)
	}
}

// A Notification about the quota is the same fact; one that says agents are resuming lifts it.
func TestE2EQuotaNotificationsOpenAndLiftTheQuota(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	note := func(kind string) string {
		return jsonString(map[string]any{"session_id": "s1", "cwd": app, "hook_event_name": "Notification", "notification_type": kind, "message": "x"})
	}
	h.hookIn(app, laneEnv("agent-a"), note("quota_auto_resume_scheduled"), "hook", "claude-flow")
	if s := h.flowSnapshot(app); s.Quota == nil || s.Quota.Agents != 1 || len(s.NeedsYou) != 1 {
		t.Fatalf("%+v %+v", s.Quota, s.NeedsYou)
	}
	h.hookIn(app, laneEnv("agent-a"), note("quota_auto_resume_started"), "hook", "claude-flow")
	if s := h.flowSnapshot(app); s.Quota != nil || len(s.NeedsYou) != 0 {
		t.Fatalf("a resume notice lifts it: %+v %+v", s.Quota, s.NeedsYou)
	}
}

// The usage limit is the account's, not the repository's: activity in another repository lifts it too.
func TestE2EActivityInAnotherRepositoryLiftsTheQuota(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	other := gitRepoAt(t, h, "other")
	h.hookIn(app, laneEnv("agent-a"), stopFailureJSON(app, "s1", "rate_limit"), "hook", "claude-flow")
	h.hookIn(other, laneEnv("agent-x", quotaGraceEnv+"=0s"), postToolUseJSON(other, "s7"), "hook", "claude-flow")
	if s := h.flowSnapshot(app); s.Quota != nil {
		t.Fatalf("still stopped: %+v", s.Quota)
	}
}

// Other API errors still wait per lane (a person may be needed), and the session's next activity clears them even when
// it is recorded under another lane: the lane of a hook follows the work tree, which an agent may leave.
func TestE2EBlockClearsWhenTheSessionRunsUnderAnotherLane(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	h.hookIn(app, laneEnv("lane-a"), stopFailureJSON(app, "s1", "overloaded"), "hook", "claude-flow")
	if s := h.flowSnapshot(app); len(s.NeedsYou) != 1 || s.NeedsYou[0].Lane != "lane-a" {
		t.Fatalf("%+v", s.NeedsYou)
	}
	if rc, out, errOut := h.inDir(app, flowOn, "4", "--lane", "lane-b", "--session", "s1", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d %s%s", rc, out, errOut)
	}
	h.mustRun(app, 0, "flow", "refresh")
	if s := h.flowSnapshot(app); len(s.NeedsYou) != 0 {
		t.Fatalf("a run of the same session under another lane means it went on: %+v", s.NeedsYou)
	}
}

// A lane whose work tree was removed (an agent's, after its pull request merged) is gone from the state.
func TestE2ELanesOfRemovedWorktreesDropOutOfTheState(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	wt := filepath.Join(h.root, "agent-gone")
	gitIn(t, h, app, "worktree", "add", "-q", "-b", "agent-gone-branch", wt)
	if rc, out, errOut := h.hookIn(wt, flowOn, permissionRequestJSON(wt, "s5", "npm test"), "hook", "claude-flow"); rc != 0 || out != "" || errOut != "" {
		t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
	}
	if s := h.flowSnapshot(app); len(s.NeedsYou) != 1 || s.NeedsYou[0].Lane != "agent-gone" || len(s.Lanes) != 1 {
		t.Fatalf("while the work tree exists: %+v %+v", s.NeedsYou, s.Lanes)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	h.mustRun(app, 0, "flow", "refresh")
	if s := h.flowSnapshot(app); len(s.NeedsYou) != 0 || len(s.Lanes) != 0 {
		t.Fatalf("a removed work tree must drop out: %+v %+v", s.NeedsYou, s.Lanes)
	}
}
