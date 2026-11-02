package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The plugin and the settings hook are the same hook; installing both would run it twice. The installer
// must refuse (exit 2, with a next: line) when any readable scope enables heavyq@heavyq, unless --force.
func TestE2EHookInstallRefusesWhenPluginEnabled(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := gitRepo(t, h)
	h.cwd = repo
	userDir := filepath.Join(h.root, "claude-home")
	h.env = append(h.env, "CLAUDE_CONFIG_DIR="+userDir)
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userSettings := filepath.Join(userDir, "settings.json")
	local := filepath.Join(repo, ".claude", "settings.local.json")

	// Disabled or absent: installs as before.
	if err := os.WriteFile(userSettings, []byte(`{"enabledPlugins":{"heavyq@heavyq":false,"other@x":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("hook", "install", "claude"); rc != 0 {
		t.Fatalf("disabled plugin must not block: rc=%d %s", rc, out)
	}
	if rc, _ := h.run("hook", "uninstall", "claude"); rc != 0 {
		t.Fatal("uninstall failed")
	}

	// Enabled at user scope: refused for every target scope, nothing written.
	if err := os.WriteFile(userSettings, []byte(`{"enabledPlugins":{"heavyq@heavyq":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"local", "project", "user"} {
		rc, out := h.run("hook", "install", "claude", "--scope", scope)
		if rc != ExitUsage || !strings.Contains(out, "heavyq@heavyq") || !strings.Contains(out, userSettings) || !strings.Contains(out, "next:") || !strings.Contains(out, "--force") {
			t.Fatalf("scope %s: want refusal with next:, rc=%d %s", scope, rc, out)
		}
	}
	for _, f := range []string{local, filepath.Join(repo, ".claude", "settings.json")} {
		if b, _ := os.ReadFile(f); strings.Contains(string(b), "claude-pretooluse") {
			t.Fatalf("a refused install must not write %s:\n%s", f, b)
		}
	}
	if b, _ := os.ReadFile(userSettings); !strings.Contains(string(b), `"heavyq@heavyq":true`) || strings.Contains(string(b), "claude-pretooluse") {
		t.Fatalf("a refused install must leave user settings alone:\n%s", b)
	}
	if rc, out := h.run("hook", "install", "claude", "--print"); rc != 0 || !strings.Contains(out, "claude-pretooluse") {
		t.Fatalf("--print writes nothing and is allowed: rc=%d %s", rc, out)
	}

	// --force installs anyway.
	if rc, out := h.run("hook", "install", "claude", "--force"); rc != 0 || !strings.Contains(out, "installed") {
		t.Fatalf("--force: rc=%d %s", rc, out)
	}

	// Enabled only in the project's shared settings: also found.
	if err := os.WriteFile(userSettings, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, _ := h.run("hook", "uninstall", "claude"); rc != 0 {
		t.Fatal("uninstall failed")
	}
	project := filepath.Join(repo, ".claude", "settings.json")
	if err := os.WriteFile(project, []byte(`{"enabledPlugins":{"heavyq@heavyq":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("hook", "install", "claude", "--scope", "user"); rc != ExitUsage || !strings.Contains(out, project) {
		t.Fatalf("project-scope plugin must refuse: rc=%d %s", rc, out)
	}
}
