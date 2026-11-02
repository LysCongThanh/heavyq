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

// steerAnchor is the anchor of the steer e2e tests: one item proven by the gate "ready", one more that is open.
const steerAnchor = "# Goal: ship login v2\n\n" +
	"- [ ] login-api: POST /login returns a token  `proof: gate:ready`\n" +
	"- [ ] docs-api: document the endpoints\n"

// steerRepo is a committed repository whose gate "ready" is red while red.flag exists, with steerAnchor.
// h.cwd is the repository. The lane is "app", the work tree's name.
func steerRepo(t *testing.T, h *rig) string {
	t.Helper()
	app := gitRepo(t, h)
	for name, content := range map[string]string{
		"heavyq.toml":       "[[gate]]\nname = \"ready\"\nrun = [\"test ! -f red.flag\"]\n",
		".heavyq/anchor.md": steerAnchor,
	} {
		p := filepath.Join(app, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, h, app, "add", ".")
	gitIn(t, h, app, "commit", "-q", "-m", "anchor and gate")
	return app
}

// hookIn runs a heavyq hook in dir with stdin and extra environment: exit code, stdout, stderr.
func (h *rig) hookIn(dir string, env []string, stdin string, args ...string) (int, string, string) {
	h.t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir, cmd.Env = dir, append(append([]string{}, h.env...), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	return processExit(cmd.Run()), out.String(), errb.String()
}

func jsonString(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// sessionStartJSON is what Claude Code sends a SessionStart hook.
func sessionStartJSON(cwd, session, source, transcript string) string {
	return jsonString(map[string]any{"session_id": session, "cwd": cwd, "hook_event_name": "SessionStart",
		"source": source, "transcript_path": transcript, "model": "claude-x"})
}

// stopJSON is what Claude Code sends the stop-family hooks.
func stopJSON(cwd, session, event string, active bool, lastMessage string) string {
	return jsonString(map[string]any{"session_id": session, "cwd": cwd, "hook_event_name": event,
		"stop_hook_active": active, "last_assistant_message": lastMessage, "transcript_path": "/nowhere/t.jsonl"})
}

// additionalContextOf returns hookSpecificOutput.additionalContext and its event name from a hook's stdout.
func additionalContextOf(t *testing.T, out string) (event, text string) {
	t.Helper()
	var v struct {
		H struct {
			Event string `json:"hookEventName"`
			Text  string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not hook JSON: %v\n%q", err, out)
	}
	return v.H.Event, v.H.Text
}

// blockReasonOf returns the reason of a {"decision":"block"} output.
func blockReasonOf(t *testing.T, out string) string {
	t.Helper()
	var v struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Decision != "block" {
		t.Fatalf("want a block decision, got %v %q (%v)", v, out, err)
	}
	return v.Reason
}

// mustRun runs heavyq in dir and fails unless it exits want.
func (h *rig) mustRun(dir string, want int, args ...string) string {
	h.t.Helper()
	rc, out, errOut := h.inDir(dir, nil, args...)
	if rc != want {
		h.t.Fatalf("heavyq %v: rc=%d want %d\n%s%s", args, rc, want, out, errOut)
	}
	return out + errOut
}
