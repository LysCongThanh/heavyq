package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The brief carries the lane's active stance, so a stance survives compaction:
// SessionStart (source compact) injects the brief and then the stance's compact form.
func TestE2ESessionStartBriefCarriesTheActiveStance(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "XDG_CONFIG_HOME="+filepath.Join(h.root, "xdg"))
	app := steerRepo(t, h)
	start := func(source string) string {
		t.Helper()
		rc, out, errOut := h.hookIn(app, nil, sessionStartJSON(app, "s1", source, ""), "hook", "claude-sessionstart")
		if rc != 0 || errOut != "" {
			t.Fatalf("rc=%d stderr=%q", rc, errOut)
		}
		return out
	}
	if _, text := additionalContextOf(t, start("compact")); strings.Contains(text, "stance") {
		t.Fatalf("no stance entered yet, the brief must not mention one:\n%s", text)
	}

	h.mustRun(app, 0, "stance", "enter", "breakthrough", "--why", "login keeps failing")
	event, text := additionalContextOf(t, start("compact"))
	if event != "SessionStart" || !strings.Contains(text, "goal: ship login v2") ||
		!strings.Contains(text, "[heavyq · stance breakthrough] why now: login keeps failing") {
		t.Fatalf("after compaction the brief must carry the brief and the stance:\n%s", text)
	}
	if !strings.HasSuffix(text, "full: heavyq stance show breakthrough") {
		t.Errorf("the stance comes after the brief:\n%s", text)
	}

	// Off, muted and a stance that was left are not re-injected.
	if out := h.mustRun(app, 0, "stance", "off"); !strings.Contains(out, "off") {
		t.Fatal(out)
	}
	if _, text := additionalContextOf(t, start("compact")); strings.Contains(text, "stance") {
		t.Errorf("a muted lane re-injected its stance:\n%s", text)
	}
	h.mustRun(app, 0, "stance", "enter", "calm")
	if rc, out, _ := h.hookIn(app, []string{"HEAVYQ_STANCES=off"}, sessionStartJSON(app, "s1", "compact", ""), "hook", "claude-sessionstart"); rc != 0 || strings.Contains(out, "stance calm") {
		t.Errorf("HEAVYQ_STANCES=off still injected: rc=%d %s", rc, out)
	}

	// A repository with no brief at all still gets the stance (stance only).
	plain := gitRepoAt(t, h, "plain")
	if rc, out, _ := h.hookIn(plain, nil, sessionStartJSON(plain, "s2", "compact", ""), "hook", "claude-sessionstart"); rc != 0 || out != "" {
		t.Fatalf("no stance, no brief: rc=%d %q", rc, out)
	}
	h.mustRun(plain, 0, "stance", "enter", "calm")
	_, out, _ := h.hookIn(plain, nil, sessionStartJSON(plain, "s2", "compact", ""), "hook", "claude-sessionstart")
	if _, text := additionalContextOf(t, out); !strings.HasPrefix(text, "[heavyq · stance calm]") {
		t.Errorf("stance-only context:\n%s", text)
	}
}

// `hook install claude --features stance` registers UserPromptSubmit, and the
// hook the settings name is the one that enters stances.
func TestE2EHookInstallStanceFeature(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	h.env = append(h.env, "CLAUDE_CONFIG_DIR="+filepath.Join(h.root, "claude-home"))
	out := h.mustRun(repo, 0, "hook", "install", "claude", "--features", "queue,stance")
	if !strings.Contains(out, "installed features queue,stance") {
		t.Fatalf("install output:\n%s", out)
	}
	b, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	ups := v.Hooks["UserPromptSubmit"]
	if len(ups) != 1 || len(ups[0].Hooks) != 1 || ups[0].Hooks[0].Command != binPath+" hook claude-userpromptsubmit" || len(v.Hooks) != 2 {
		t.Fatalf("settings:\n%s", b)
	}
	if out := h.mustRun(repo, 0, "hook", "install", "claude", "--features", "stance,queue"); !strings.Contains(out, "already installed") {
		t.Fatalf("same features in another order must be a no-op: %s", out)
	}
	h.mustRun(repo, 0, "hook", "uninstall", "claude")
	if b, _ := os.ReadFile(filepath.Join(repo, ".claude", "settings.local.json")); strings.Contains(string(b), "userpromptsubmit") {
		t.Errorf("uninstall left the stance hook:\n%s", b)
	}
}
