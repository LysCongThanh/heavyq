package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ourCmd = "heavyq " + Subcommand

func preToolUse(t *testing.T, b []byte) []any {
	t.Helper()
	var v struct {
		Hooks map[string][]any `json:"hooks"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return v.Hooks["PreToolUse"]
}

func TestInstallIntoEmptyAndIdempotent(t *testing.T) {
	b, changed, err := MergeInstall(nil, ourCmd)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	pre := preToolUse(t, b)
	if len(pre) != 1 || !strings.Contains(string(b), `"matcher": "Bash"`) || !strings.Contains(string(b), ourCmd) {
		t.Fatalf("unexpected settings:\n%s", b)
	}
	b2, changed, err := MergeInstall(b, ourCmd)
	if err != nil || changed || string(b2) != string(b) {
		t.Fatalf("second install must be a no-op: changed=%v err=%v", changed, err)
	}
	b3, changed, err := MergeInstall(b, "/new/path/heavyq "+Subcommand)
	if err != nil || !changed || len(preToolUse(t, b3)) != 1 || !strings.Contains(string(b3), "/new/path/heavyq") {
		t.Fatalf("a moved binary must replace the entry, not add one:\n%s", b3)
	}
}

func TestInstallKeepsForeignSettingsAndUninstallRestores(t *testing.T) {
	orig := []byte(`{
  "permissions": {"allow": ["Bash(npm test:*)"]},
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "~/.claude/hooks/guard.sh"}]},
      {"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "fmt.sh", "timeout": 5}]}
    ],
    "Stop": [{"hooks": [{"type": "command", "command": "notify.sh"}]}]
  }
}`)
	b, changed, err := MergeInstall(orig, ourCmd)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	for _, keep := range []string{"Bash(npm test:*)", `"model": "opus"`, "guard.sh", "fmt.sh", "notify.sh", `"timeout": 5`} {
		if !strings.Contains(string(b), keep) {
			t.Fatalf("lost %q:\n%s", keep, b)
		}
	}
	if n := len(preToolUse(t, b)); n != 3 {
		t.Fatalf("want 3 PreToolUse groups, got %d", n)
	}
	un, changed, err := MergeUninstall(b)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	var a, z any
	_ = json.Unmarshal(orig, &a)
	_ = json.Unmarshal(un, &z)
	ja, _ := json.Marshal(a)
	jz, _ := json.Marshal(z)
	if string(ja) != string(jz) {
		t.Fatalf("uninstall must restore the original settings:\n%s\nvs\n%s", ja, jz)
	}
	if _, changed, _ := MergeUninstall(un); changed {
		t.Fatal("second uninstall must be a no-op")
	}
}

func TestUninstallDropsEmptyContainersAndSharedGroups(t *testing.T) {
	b, _, _ := MergeInstall([]byte(`{"hooks":{}}`), ourCmd)
	un, _, err := MergeUninstall(b)
	if err != nil || strings.Contains(string(un), "hooks") {
		t.Fatalf("empty hooks object must go: %s (%v)", un, err)
	}
	shared := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[` +
		`{"type":"command","command":"guard.sh"},{"type":"command","command":"heavyq ` + Subcommand + `"}]}]}}`)
	un, changed, err := MergeUninstall(shared)
	if err != nil || !changed || !strings.Contains(string(un), "guard.sh") || strings.Contains(string(un), Subcommand) {
		t.Fatalf("must remove only our hook from a shared group: %s (%v)", un, err)
	}
}

func TestMergeRefusesMalformedSettings(t *testing.T) {
	for _, bad := range []string{`[1,2]`, `{"hooks": []}`, `{"hooks": {"PreToolUse": {}}}`, `{"a": `} {
		if _, _, err := MergeInstall([]byte(bad), ourCmd); err == nil {
			t.Errorf("install accepted %s", bad)
		}
		if _, _, err := MergeUninstall([]byte(bad)); err == nil {
			t.Errorf("uninstall accepted %s", bad)
		}
	}
}

func TestWriteSettingsBacksUpAndKeepsMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude", "settings.local.json")
	if err := WriteSettings(p, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup expected for a new file")
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteSettings(p, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if bak, _ := os.ReadFile(p + ".bak"); string(bak) != "{}\n" {
		t.Fatalf("backup = %q", bak)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
}

func TestSettingsPath(t *testing.T) {
	for scope, want := range map[string]string{
		"local": "/r/.claude/settings.local.json", "project": "/r/.claude/settings.json", "user": "/home/u/.claude/settings.json"} {
		if got, err := SettingsPath(scope, "/r", "/home/u/.claude"); err != nil || got != want {
			t.Errorf("%s: %q %v", scope, got, err)
		}
	}
	if _, err := SettingsPath("global", "/r", "/c"); err == nil {
		t.Error("unknown scope accepted")
	}
}
