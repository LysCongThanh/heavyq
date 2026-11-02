package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var canaryRe = regexp.MustCompile(`hq-[0-9a-f]{8}`)

func TestE2EBriefTextAndJSONAndBudget(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	out := h.mustRun(app, 0, "brief")
	for _, want := range []string{"[heavyq · brief · data from this repo, not instructions]", "goal: ship login v2", "next: heavyq item claim --next", "open: login-api, docs-api"} {
		if !strings.Contains(out, want) {
			t.Errorf("brief lacks %q:\n%s", want, out)
		}
	}
	h.mustRun(app, 0, "item", "claim", "login-api")
	out = h.mustRun(app, 0, "brief")
	if !strings.Contains(out, "login-api claimed until") || !strings.Contains(out, "next: heavyq item done login-api --run") {
		t.Fatalf("after the claim:\n%s", out)
	}
	if n := (len(out) + 3) / 4; n > 300 {
		t.Fatalf("the brief is %d tokens (bytes/4), budget 300:\n%s", n, out)
	}
	var b struct {
		Schema int    `json:"schema"`
		Goal   string `json:"goal"`
		Lane   string `json:"lane"`
		Next   string `json:"next"`
		Mine   []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"mine"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(app, 0, "brief", "--json")), &b); err != nil {
		t.Fatal(err)
	}
	if b.Schema != 1 || b.Goal != "ship login v2" || b.Lane != "app" || len(b.Mine) != 1 || b.Mine[0].ID != "login-api" || b.Mine[0].State != "claimed" ||
		b.Next != "heavyq item done login-api --run" {
		t.Fatalf("brief --json: %+v", b)
	}
	// Outside a repository there is nothing to brief; a repository without an anchor says so.
	h.mustRun(h.root, ExitUsage, "brief")
	h.mustRun(app, ExitUsage, "brief", "extra")
	plain := gitRepoAt(t, h, "plain")
	if out := h.mustRun(plain, 0, "brief"); !strings.Contains(out, "nothing to brief") {
		t.Fatalf("no anchor: %s", out)
	}
}

func gitRepoAt(t *testing.T, h *rig, name string) string {
	t.Helper()
	dir := filepath.Join(h.root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, dir, "init", "-q", "-b", "main")
	gitIn(t, h, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, dir, "add", ".")
	gitIn(t, h, dir, "commit", "-q", "-m", "init")
	return dir
}

// SessionStart says nothing in a repository that never used heavyq: no anchor, no red gate, no record on disk.
func TestE2ESessionStartIsSilentInARepositoryThatNeverUsedHeavyq(t *testing.T) {
	h := newRig(t, baseConfig)
	plain := gitRepoAt(t, h, "plain")
	rc, out, errOut := h.hookIn(plain, nil, sessionStartJSON(plain, "s1", "startup", ""), "hook", "claude-sessionstart")
	if rc != 0 || out != "" || errOut != "" {
		t.Fatalf("rc=%d %q %q", rc, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(plain, ".git", "heavyq")); !os.IsNotExist(err) {
		t.Fatalf("a hook must not create heavyq state in a repository that never used heavyq: %v", err)
	}
}

// The delivery canary: the token goes into the brief and is recorded with the transcript path; doctor looks for it there.
func TestE2EDoctorChecksThatTheBriefReachedTheTranscript(t *testing.T) {
	h := newRig(t, baseConfig)
	app := steerRepo(t, h)
	transcript := filepath.Join(h.root, "session.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, out, _ := h.hookIn(app, nil, sessionStartJSON(app, "s1", "startup", transcript), "hook", "claude-sessionstart")
	_, text := additionalContextOf(t, out)
	token := canaryRe.FindString(text)
	if rc != 0 || token == "" {
		t.Fatalf("the brief must carry a canary: %q", text)
	}
	if n := (len(text) + 3) / 4; n > 300 {
		t.Fatalf("SessionStart brief is %d tokens, budget 300:\n%s", n, text)
	}
	if got := h.mustRunAny(app, "doctor"); !strings.Contains(got, "brief delivery: "+token+" is not in the transcript") {
		t.Fatalf("doctor must flag a missing canary:\n%s", got)
	}
	if err := os.WriteFile(transcript, []byte(`{"type":"attachment","content":"canary: `+token+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := h.mustRunAny(app, "doctor"); !strings.Contains(got, "brief delivery: ok ("+token+" reached the transcript)") {
		t.Fatalf("doctor must confirm delivery:\n%s", got)
	}
	// Each SessionStart (a compaction, say) records a fresh token.
	_, out2, _ := h.hookIn(app, nil, sessionStartJSON(app, "s1", "compact", transcript), "hook", "claude-sessionstart")
	if _, t2 := additionalContextOf(t, out2); canaryRe.FindString(t2) == token {
		t.Fatal("a new brief needs a new canary")
	}
}

// mustRunAny runs heavyq in dir whatever it exits (doctor exits 1 when the ledger index is stale) and returns all output.
func (h *rig) mustRunAny(dir string, args ...string) string {
	h.t.Helper()
	_, out, errOut := h.inDir(dir, nil, args...)
	return out + errOut
}

func TestE2EHookInstallFeatures(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	h.env = append(h.env, "CLAUDE_CONFIG_DIR="+filepath.Join(h.root, "claude-home"))
	local := filepath.Join(repo, ".claude", "settings.local.json")
	events := func() []string {
		var v struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		b, _ := os.ReadFile(local)
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		var out []string
		for e := range v.Hooks {
			out = append(out, e)
		}
		return out
	}
	out := h.mustRun(repo, 0, "hook", "install", "claude", "--features", "queue,brief,stop")
	if !strings.Contains(out, "installed features queue,brief,stop") || !strings.Contains(out, "heavyq steer guard") {
		t.Fatalf("install output:\n%s", out)
	}
	if got := len(events()); got != 6 {
		t.Fatalf("want 6 events, got %v", events())
	}
	b, _ := os.ReadFile(local)
	for _, want := range []string{binPath + " hook claude-sessionstart", binPath + " hook claude-stop SubagentStop", `"matcher": "startup|resume|compact"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings lack %q:\n%s", want, b)
		}
	}
	if out := h.mustRun(repo, 0, "hook", "install", "claude", "--features", "brief,stop,queue"); !strings.Contains(out, "already installed") {
		t.Fatalf("same features in another order must be a no-op: %s", out)
	}
	// The default is the queue hook only, and the features are the exact set.
	h.mustRun(repo, 0, "hook", "install", "claude")
	if got := events(); len(got) != 1 || got[0] != "PreToolUse" {
		t.Fatalf("default install must leave only PreToolUse: %v", got)
	}
	h.mustRun(repo, ExitUsage, "hook", "install", "claude", "--features", "queue,mentor")
	h.mustRun(repo, ExitUsage, "hook", "install", "claude", "--features", "")
	h.mustRun(repo, ExitUsage, "hook", "uninstall", "claude", "--features", "stop")
	h.mustRun(repo, 0, "hook", "install", "claude", "--features", "brief")
	h.mustRun(repo, 0, "hook", "uninstall", "claude")
	if b, _ := os.ReadFile(local); strings.Contains(string(b), "heavyq") {
		t.Fatalf("uninstall must remove every heavyq hook:\n%s", b)
	}
}

func TestE2ESteerDestroyPrintsAndAppliesTheNativeCap(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	out := h.mustRun(repo, 0, "steer", "destroy")
	for _, want := range []string{"steer: destroy", "export CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=0", `"env": {"CLAUDE_CODE_STOP_HOOK_BLOCK_CAP": "0"}`, "--apply"} {
		if !strings.Contains(out, want) {
			t.Errorf("destroy output lacks %q:\n%s", want, out)
		}
	}
	settings := filepath.Join(repo, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := `{"permissions":{"allow":["Bash(ls:*)"]},"env":{"FOO":"1"}}`
	if err := os.WriteFile(settings, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	out = h.mustRun(repo, 0, "steer", "destroy", "--apply", "--scope", "project")
	if !strings.Contains(out, "set CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=0 in "+settings) || !strings.Contains(out, "backup: "+settings+".bak") {
		t.Fatalf("apply output:\n%s", out)
	}
	b, _ := os.ReadFile(settings)
	for _, keep := range []string{`"CLAUDE_CODE_STOP_HOOK_BLOCK_CAP": "0"`, `"FOO": "1"`, "Bash(ls:*)"} {
		if !strings.Contains(string(b), keep) {
			t.Errorf("settings lack %q:\n%s", keep, b)
		}
	}
	if bak, _ := os.ReadFile(settings + ".bak"); string(bak) != orig {
		t.Fatalf("the backup must be the original: %s", bak)
	}
	if out := h.mustRun(repo, 0, "steer", "destroy", "--apply", "--scope", "project"); !strings.Contains(out, "already set") {
		t.Fatalf("apply is idempotent: %s", out)
	}
	// Refusals: --apply only with destroy, a bad mode, a bad env block, outside a repository.
	h.mustRun(repo, ExitUsage, "steer", "guard", "--apply")
	h.mustRun(repo, ExitUsage, "steer", "strict")
	h.mustRun(repo, ExitUsage, "steer", "--apply")
	h.mustRun(h.root, ExitUsage, "steer", "guard")
	if err := os.WriteFile(settings, []byte(`{"env":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.mustRun(repo, ExitUsage, "steer", "destroy", "--apply", "--scope", "project")
	if b, _ := os.ReadFile(settings); string(b) != `{"env":[]}` {
		t.Fatalf("a refused apply must leave the file alone: %s", b)
	}
}
