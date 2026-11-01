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

// stanceRig is a rig inside a real git repository, with the user's stance
// directory under the scratch dir (never the real ~/.config).
type stanceRig struct {
	*rig
	repo, userDir string
}

func newStanceRig(t *testing.T) *stanceRig {
	t.Helper()
	h := newRig(t, baseConfig)
	repo := filepath.Join(h.root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	xdg := filepath.Join(h.root, "xdg")
	h.env = append(h.env, "XDG_CONFIG_HOME="+xdg)
	h.cwd = filepath.Join(repo, "sub") // the repo directory is the git toplevel, not the cwd
	return &stanceRig{rig: h, repo: repo, userDir: filepath.Join(xdg, "heavyq", "stances")}
}

func (h *stanceRig) put(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// split runs heavyq keeping stdout and stderr apart.
func (h *stanceRig) split(args ...string) (rc int, stdout, stderr string) {
	cmd := exec.Command(binPath, args...)
	cmd.Dir, cmd.Env = h.cwd, h.env
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	rc = processExit(cmd.Run())
	return rc, o.String(), e.String()
}

func TestStanceLsAndShowBuiltins(t *testing.T) {
	h := newStanceRig(t)
	rc, out, errOut := h.split("stance", "ls")
	if rc != 0 || errOut != "" {
		t.Fatalf("ls rc=%d stderr=%q", rc, errOut)
	}
	for _, want := range []string{"NAME", "SOURCE", "DESCRIPTION", "breakthrough  builtin", "calm", "converge", "skeptic", "surgeon"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls output lacks %q:\n%s", want, out)
		}
	}
	rc, out, _ = h.split("stance", "show", "breakthrough")
	if rc != 0 || !strings.Contains(out, "Starting from scratch") || strings.Contains(out, "+++") {
		t.Fatalf("show rc=%d:\n%s", rc, out)
	}
}

func TestStanceOverridesAndSourcePath(t *testing.T) {
	h := newStanceRig(t)
	userCalm := h.put(t, h.userDir, "calm.md", "+++\ndescription = \"user calm\"\n+++\nuser calm body\n")
	repoCalm := h.put(t, filepath.Join(h.repo, ".heavyq", "stances"), "calm.md", "+++\ndescription = \"repo calm\"\n+++\nrepo calm body\n")
	// A Vietnamese stance, reachable by name from a subdirectory of the repo.
	viet := "Hãy lùi lại và nghĩ từ gốc vấn đề."
	h.put(t, h.userDir, "dot-pha.md", "+++\ndescription = \"Tư duy đột phá\"\n+++\n"+viet+"\n")

	rc, out, _ := h.split("stance", "ls")
	if rc != 0 || !strings.Contains(out, "repo "+repoCalm) || strings.Contains(out, userCalm) || !strings.Contains(out, "Tư duy đột phá") {
		t.Fatalf("ls rc=%d:\n%s", rc, out)
	}
	if rc, out, _ = h.split("stance", "show", "calm"); rc != 0 || out != "repo calm body\n" {
		t.Fatalf("show calm rc=%d out=%q", rc, out)
	}
	if rc, out, _ = h.split("stance", "show", "dot-pha"); rc != 0 || out != viet+"\n" {
		t.Fatalf("show dot-pha rc=%d out=%q", rc, out)
	}
}

func TestStanceJSON(t *testing.T) {
	h := newStanceRig(t)
	rc, out, errOut := h.split("stance", "ls", "--json")
	if rc != 0 || errOut != "" {
		t.Fatalf("rc=%d stderr=%q", rc, errOut)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list) != 5 {
		t.Fatalf("ls --json: %v (%d entries)\n%s", err, len(list), out)
	}
	if list[0]["name"] != "breakthrough" || list[0]["source"] != "builtin" || list[0]["path"] != "builtin:breakthrough" {
		t.Fatalf("first entry = %v", list[0])
	}
	rc, out, _ = h.split("stance", "show", "--json", "breakthrough") // flag before the name
	var one struct {
		Name, Source, Body string
		Next               string
		Enter              struct {
			SameFailure int `json:"same_failure_class_across_trees"`
		}
	}
	if err := json.Unmarshal([]byte(out), &one); rc != 0 || err != nil || one.Name != "breakthrough" || one.Enter.SameFailure != 3 || one.Next == "" || !strings.Contains(one.Body, "from scratch") {
		t.Fatalf("show --json rc=%d err=%v:\n%s", rc, err, out)
	}
}

func TestStanceErrors(t *testing.T) {
	h := newStanceRig(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"stance"}, "usage:"},
		{[]string{"stance", "nope"}, `unknown stance command "nope"`},
		{[]string{"stance", "show"}, "usage:"},
		{[]string{"stance", "ls", "extra"}, "usage:"},
		{[]string{"stance", "show", "missing"}, `no stance named "missing" (known: breakthrough, calm, converge, skeptic, surgeon)`},
	} {
		if rc, _, errOut := h.split(c.args...); rc != ExitUsage || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: rc=%d stderr=%q, want exit 2 and %q", c.args, rc, errOut, c.want)
		}
	}
	bad := h.put(t, filepath.Join(h.repo, ".heavyq", "stances"), "calm.md", "+++\nbogus = 1\n+++\nx\n")
	if rc, _, errOut := h.split("stance", "ls"); rc != ExitUsage || !strings.Contains(errOut, bad+":2: unknown key") {
		t.Fatalf("broken file: rc=%d stderr=%q", rc, errOut)
	}
}
