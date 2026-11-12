package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hookResponse runs the PreToolUse hook on a Bash call and decodes its answer
// (zero values when it printed nothing).
func hookResponse(t *testing.T, h *rig, toolInput map[string]any) (cmd string, timeout float64, context string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"session_id": "s", "cwd": h.root, "hook_event_name": "PreToolUse",
		"tool_name": "Bash", "tool_input": toolInput})
	rc, out, errOut := h.runStdin(string(b), "hook", "claude-pretooluse")
	if rc != 0 || errOut != "" {
		t.Fatalf("rc=%d stderr=%s", rc, errOut)
	}
	if out == "" {
		return "", 0, ""
	}
	var resp struct {
		H struct {
			U       map[string]any `json:"updatedInput"`
			Context string         `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	cmd, _ = resp.H.U["command"].(string)
	timeout, _ = resp.H.U["timeout"].(float64)
	return cmd, timeout, resp.H.Context
}

const shRule = "[hook]\nmax_wait = \"60s\"\n[[hook.rule]]\nmatch = [\"sh -c\"]\n"

// The tool timeout follows Claude Code's own default and cap
// (BASH_DEFAULT_TIMEOUT_MS, BASH_MAX_TIMEOUT_MS): the hook never lowers it,
// never writes more than the cap, and says when queue time comes out of it.
func TestE2EHookToolTimeoutFollowsClaudeCodeLimits(t *testing.T) {
	h := newRig(t, baseConfig+shRule)
	call := map[string]any{"command": "sh -c 'echo x'"}
	base := h.env
	h.env = append(base, "BASH_DEFAULT_TIMEOUT_MS=1800000")
	if _, timeout, _ := hookResponse(t, h, call); timeout != 1800000 {
		t.Fatalf("default 30 min: timeout %v, want 1800000 (never below the default, never over the cap)", timeout)
	}
	h.env = base
	if _, timeout, ctx := hookResponse(t, h, map[string]any{"command": "sh -c 'echo x'", "timeout": 600000}); timeout != 600000 ||
		!strings.Contains(ctx, "capped") {
		t.Fatalf("timeout 600000 at the cap: got %v, context %q", timeout, ctx)
	}
	h.env = append(base, "BASH_MAX_TIMEOUT_MS=900000")
	if _, timeout, ctx := hookResponse(t, h, map[string]any{"command": "sh -c 'echo x'", "timeout": 600000}); timeout != 660000 ||
		strings.Contains(ctx, "capped") {
		t.Fatalf("room under a raised cap: got %v, context %q", timeout, ctx)
	}
}

// A program word that PATH does not find (an alias or function in the
// agent's shell) is left to the shell instead of failing with 127 in heavyq.
func TestE2EHookLeavesProgramsPathDoesNotFind(t *testing.T) {
	h := newRig(t, baseConfig+"[[hook.rule]]\nmatch = [\"hqfakeprog test\"]\n")
	if cmd, _, _ := hookResponse(t, h, map[string]any{"command": "hqfakeprog test"}); cmd != "" {
		t.Fatalf("a program not on PATH was wrapped: %q", cmd)
	}
	if log := h.read("hook.log"); !strings.Contains(log, "not on PATH") {
		t.Fatalf("hook.log does not say why:\n%s", log)
	}
	bin := filepath.Join(h.root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "hqfakeprog"), []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, e := range h.env {
		if strings.HasPrefix(e, "PATH=") {
			h.env[i] = "PATH=" + bin + string(os.PathListSeparator) + strings.TrimPrefix(e, "PATH=")
		}
	}
	if cmd, _, _ := hookResponse(t, h, map[string]any{"command": "hqfakeprog test"}); !strings.Contains(cmd, " -- hqfakeprog test") {
		t.Fatalf("a program on PATH must be wrapped: %q", cmd)
	}
}

// hook.max_wait under a second is refused (the hook fails open and logs it)
// instead of becoming --max-wait 0s, which means wait forever.
func TestE2EHookRefusesSubSecondMaxWait(t *testing.T) {
	h := newRig(t, baseConfig+"[hook]\nmax_wait = \"400ms\"\n[[hook.rule]]\nmatch = [\"sh -c\"]\n")
	if cmd, _, _ := hookResponse(t, h, map[string]any{"command": "sh -c 'echo x'"}); cmd != "" {
		t.Fatalf("a sub-second max_wait was used: %q", cmd)
	}
	if log := h.read("hook.log"); !strings.Contains(log, "hook.max_wait") {
		t.Fatalf("hook.log does not name hook.max_wait:\n%s", log)
	}
}

// hook.log never stores a secret from the command line.
func TestE2EHookLogIsRedacted(t *testing.T) {
	h := newRig(t, baseConfig+shRule)
	cmd, _, _ := hookResponse(t, h, map[string]any{"command": "API_TOKEN=hunter2secret sh -c 'echo x' --api-key=abc123secret"})
	if !strings.Contains(cmd, "API_TOKEN=hunter2secret ") {
		t.Fatalf("the command itself must run as typed: %q", cmd)
	}
	log := h.read("hook.log")
	if !strings.Contains(log, "rewritten: ") || strings.Contains(log, "hunter2secret") || strings.Contains(log, "abc123secret") {
		t.Fatalf("hook.log:\n%s", log)
	}
}
