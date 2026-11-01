package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runStdin runs heavyq with stdin and returns exit code, stdout, stderr.
func (h *rig) runStdin(stdin string, args ...string) (int, string, string) {
	h.t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir, cmd.Env = h.cwd, h.env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	return processExit(cmd.Run()), out.String(), errb.String()
}

func hookJSON(cwd, command string) string {
	b, _ := json.Marshal(map[string]any{"session_id": "s", "cwd": cwd, "hook_event_name": "PreToolUse",
		"tool_name": "Bash", "tool_input": map[string]any{"command": command, "description": "d"}})
	return string(b)
}

func TestE2EHookRewritesAndTheRewriteRuns(t *testing.T) {
	h := newRig(t, baseConfig+"[hook]\nmax_wait = \"60s\"\n[[hook.rule]]\nmatch = [\"sh -c\"]\npriority = 3\n")
	wt := filepath.Join(h.root, "wt-billing")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := h.runStdin(hookJSON(wt, "sh -c 'echo ran-in-queue $HEAVYQ_LANE' 2>&1 | tail -1"), "hook", "claude-pretooluse")
	if rc != 0 || errOut != "" {
		t.Fatalf("rc=%d stderr=%s", rc, errOut)
	}
	var resp struct {
		H struct {
			Event string         `json:"hookEventName"`
			Dec   string         `json:"permissionDecision"`
			U     map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	cmd, _ := resp.H.U["command"].(string)
	want := binPath + " 3 --lane wt-billing --max-wait 60s -- sh -c 'echo ran-in-queue $HEAVYQ_LANE' 2>&1 | tail -1"
	if resp.H.Event != "PreToolUse" || resp.H.Dec != "" || cmd != want || resp.H.U["timeout"] != 180000.0 || resp.H.U["description"] != "d" {
		t.Fatalf("unexpected response: %s\nwant command %q", out, want)
	}
	// Run the rewritten command the way Claude Code's Bash tool would.
	sh := exec.Command("bash", "-c", cmd)
	sh.Dir, sh.Env = wt, h.env
	got, err := sh.Output()
	if err != nil || strings.TrimSpace(string(got)) != "ran-in-queue wt-billing" {
		t.Fatalf("rewritten command: %v %q", err, got)
	}
	if !strings.Contains(h.read("history.jsonl"), `"lane":"wt-billing"`) {
		t.Fatal("the rewritten command did not go through the queue")
	}
	if !strings.Contains(h.read("hook.log"), "rewritten: ") {
		t.Fatal("the rewrite was not logged")
	}
}

func TestE2EHookLeavesUnsafeCommandsAndFailsOpen(t *testing.T) {
	h := newRig(t, baseConfig)
	for _, c := range []string{"npm test && npm run build", "npm test &", "heavyq 4 -- npm test", "ls"} {
		if rc, out, errOut := h.runStdin(hookJSON(h.root, c), "hook", "claude-pretooluse"); rc != 0 || out != "" || errOut != "" {
			t.Fatalf("%q: rc=%d out=%q err=%q", c, rc, out, errOut)
		}
	}
	h.env = append(h.env, "HEAVYQ_ACTIVE=1")
	if _, out, _ := h.runStdin(hookJSON(h.root, "npm test"), "hook", "claude-pretooluse"); out != "" {
		t.Fatalf("HEAVYQ_ACTIVE must disable the hook: %q", out)
	}
	// Garbage input and a broken config: allow unchanged (exit 0, no output) and log.
	if rc, out, _ := h.runStdin("{not json", "hook", "claude-pretooluse"); rc != 0 || out != "" {
		t.Fatalf("garbage stdin: rc=%d out=%q", rc, out)
	}
	if err := os.WriteFile(filepath.Join(h.root, "config.toml"), []byte("[hook]\npermission = \"maybe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, out, _ := h.runStdin(hookJSON(h.root, "npm test"), "hook", "claude-pretooluse"); rc != 0 || out != "" {
		t.Fatalf("bad config: rc=%d out=%q", rc, out)
	}
	if log := h.read("hook.log"); !strings.Contains(log, "not hook JSON") || !strings.Contains(log, "permission") {
		t.Fatalf("errors not logged:\n%s", log)
	}
}

func TestE2EHookInstallUninstall(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	h.cwd = filepath.Join(repo, "sub")
	if err := os.MkdirAll(h.cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	userDir := filepath.Join(h.root, "claude-home")
	h.env = append(h.env, "CLAUDE_CONFIG_DIR="+userDir)
	local := filepath.Join(repo, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte(`{"permissions":{"allow":["Bash(ls:*)"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("hook", "install", "claude"); rc != 0 || !strings.Contains(out, local) {
		t.Fatalf("install: rc=%d %s", rc, out)
	}
	b, _ := os.ReadFile(local)
	if !strings.Contains(string(b), binPath+" hook claude-pretooluse") || !strings.Contains(string(b), "Bash(ls:*)") {
		t.Fatalf("settings:\n%s", b)
	}
	if bak, _ := os.ReadFile(local + ".bak"); !strings.Contains(string(bak), "Bash(ls:*)") || strings.Contains(string(bak), "heavyq") {
		t.Fatalf("backup:\n%s", bak)
	}
	if rc, out := h.run("hook", "install", "claude"); rc != 0 || !strings.Contains(out, "already installed") {
		t.Fatalf("second install: rc=%d %s", rc, out)
	}
	if rc, out := h.run("hook", "install", "claude", "--scope", "user", "--print"); rc != 0 || !strings.Contains(out, "claude-pretooluse") {
		t.Fatalf("--print: rc=%d %s", rc, out)
	}
	if _, err := os.Stat(filepath.Join(userDir, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("--print must not write")
	}
	if rc, _ := h.run("hook", "install", "claude", "--scope", "project"); rc != 0 {
		t.Fatal("project install failed")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".claude", "settings.json")); !strings.Contains(string(b), "claude-pretooluse") {
		t.Fatalf("project settings:\n%s", b)
	}
	if rc, out := h.run("hook", "uninstall", "claude"); rc != 0 || !strings.Contains(out, "removed") {
		t.Fatalf("uninstall: rc=%d %s", rc, out)
	}
	if b, _ := os.ReadFile(local); strings.Contains(string(b), "heavyq") || !strings.Contains(string(b), "Bash(ls:*)") {
		t.Fatalf("after uninstall:\n%s", b)
	}
	if err := os.WriteFile(local, []byte(`{"hooks": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, _ := h.run("hook", "install", "claude"); rc != ExitUsage {
		t.Fatalf("malformed settings must be refused, rc=%d", rc)
	}
	if b, _ := os.ReadFile(local); string(b) != `{"hooks": []}` {
		t.Fatal("malformed settings must be left unchanged")
	}
	if rc, _ := h.run("hook", "install", "codex"); rc != ExitUsage {
		t.Fatal("unknown agent accepted")
	}
}
