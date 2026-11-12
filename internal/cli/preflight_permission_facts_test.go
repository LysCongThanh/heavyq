package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/config"
)

func TestHookRewriterIsTheRealHookDecision(t *testing.T) {
	root := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEAVYQ_LANE", "")
	rewrite := hookRewriter(&config.Config{}, root)
	got, approved, wrapped := rewrite("go test ./...")
	if !wrapped || approved || !strings.HasPrefix(got, "heavyq 4 --lane api --max-wait ") || !strings.HasSuffix(got, " -- go test ./...") {
		t.Errorf("rewrite = %q approved=%v wrapped=%v", got, approved, wrapped)
	}
	if got, _, wrapped := rewrite("ls -la"); wrapped || got != "ls -la" {
		t.Errorf("a command with no rule is left alone: %q %v", got, wrapped)
	}
	if _, _, wrapped := rewrite("heavyq 4 -- go test ./..."); wrapped {
		t.Error("an already wrapped command is left alone")
	}
	allow := hookRewriter(&config.Config{Hook: config.HookConfig{Permission: "allow"}}, root)
	if _, approved, wrapped := allow("go test ./..."); !wrapped || !approved {
		t.Errorf("hook.permission = allow approves what it rewrites: approved=%v wrapped=%v", approved, wrapped)
	}
}

func TestPermissionResults(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "none"))
	t.Setenv("HEAVYQ_LANE", "api")
	root := t.TempDir()
	settings := func(body string) {
		if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{}

	if res := permissionResults(cfg, root, []string{"go test ./..."}); res.Skipped == "" || len(res.Found) != 0 {
		t.Errorf("no sign of Claude Code: skipped, not nagged: %+v", res)
	}

	hook := `"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"heavyq hook claude-pretooluse"}]}]}`
	// The rule matches the command as typed, but the installed hook rewrites it: it may still prompt.
	settings(`{"permissions":{"allow":["Bash(go test:*)","Bash(heavyq item:*)"]},` + hook + `}`)
	res := permissionResults(cfg, root, []string{"go test ./..."})
	if len(res.Found) != 2 || !strings.Contains(res.Found[0].Text, "`go test ./...` may prompt: the heavyq hook rewrites it to `heavyq 4 --lane api") {
		t.Fatalf("%+v", res.Found)
	}
	if !strings.Contains(res.Found[0].Fix, `"Bash(heavyq 4 --lane api --max-wait `) {
		t.Errorf("the fix allows the exact wrapped prefix: %s", res.Found[0].Fix)
	}

	// Allowing the wrapped prefix, or hook.permission = allow, clears it.
	settings(`{"permissions":{"allow":["Bash(heavyq 4 --lane api --max-wait 1s -- go test *)","Bash(heavyq item:*)"]},` + hook + `}`)
	if res := permissionResults(cfg, root, []string{"go test ./..."}); len(res.Found) != 0 {
		t.Errorf("exact wrapped prefix: %+v", res.Found)
	}
	settings(`{"permissions":{"allow":["Bash(heavyq item:*)"]},` + hook + `}`)
	approving := &config.Config{Hook: config.HookConfig{Permission: "allow"}}
	if res := permissionResults(approving, root, []string{"go test ./..."}); len(res.Found) != 0 {
		t.Errorf("hook.permission = allow: %+v", res.Found)
	}
	// A hook the config disables rewrites nothing, so the plain rule is enough.
	off := false
	settings(`{"permissions":{"allow":["Bash(go test:*)","Bash(heavyq item:*)"]},` + hook + `}`)
	if res := permissionResults(&config.Config{Hook: config.HookConfig{Enabled: &off}}, root, []string{"go test ./..."}); len(res.Found) != 0 {
		t.Errorf("hook disabled: %+v", res.Found)
	}

	// An unreadable settings file is reported and costs its rules.
	settings(`{not json`)
	res = permissionResults(cfg, root, []string{"go test ./..."})
	if len(res.Found) == 0 || !strings.Contains(res.Found[0].Text, "could not read") {
		t.Errorf("%+v", res.Found)
	}
}
