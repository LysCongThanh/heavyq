package hook

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

func TestToolTimeoutFollowsClaudeCodeDefaultAndCap(t *testing.T) {
	const wait = 5 * time.Minute
	cases := []struct {
		raw         string
		env         ToolTimeouts
		want, capMS int64
		capped      bool
		explanation string
	}{
		{"", ToolTimeouts{}, 420000, 600000, false, "built-in default plus wait"},
		{"200000", ToolTimeouts{}, 500000, 600000, false, "explicit plus wait"},
		{"600000", ToolTimeouts{}, 600000, 600000, true, "at the cap: no room for the queue"},
		{"900000", ToolTimeouts{}, 600000, 600000, true, "over the cap is the cap, as Claude Code would apply it"},
		{"", ToolTimeouts{Default: "1800000"}, 1800000, 1800000, true, "a raised default is never lowered"},
		{"", ToolTimeouts{Default: "60000", Max: "3600000"}, 360000, 3600000, false, "env default plus wait"},
		{"600000", ToolTimeouts{Max: "900000"}, 900000, 900000, false, "a raised cap leaves room"},
		{"", ToolTimeouts{Default: " 90000ms", Max: "junk"}, 390000, 600000, false, "parseInt semantics, junk ignored"},
		{"", ToolTimeouts{Default: "-5", Max: "0"}, 420000, 600000, false, "non-positive values ignored"},
		{`"x"`, ToolTimeouts{}, 420000, 600000, false, "a non-number timeout is the default"},
		{"1e300", ToolTimeouts{}, 600000, 600000, true, "an absurd timeout does not overflow"},
	}
	for _, c := range cases {
		var raw json.RawMessage
		if c.raw != "" {
			raw = json.RawMessage(c.raw)
		}
		ms, capMS, capped := toolTimeout(raw, wait, c.env)
		if ms != c.want || capMS != c.capMS || capped != c.capped {
			t.Errorf("%s: got %d/%d/%v, want %d/%d/%v", c.explanation, ms, capMS, capped, c.want, c.capMS, c.capped)
		}
	}
}

func TestDecideToolTimeoutNoteOnlyWhenCapped(t *testing.T) {
	o := testOptions()
	out, _ := decideOut(t, "go test ./...", o)
	if out.H.U["timeout"] != 420000.0 || strings.Contains(out.H.Context, "capped") {
		t.Fatalf("default: %v %q", out.H.U["timeout"], out.H.Context)
	}
	o.ToolTimeouts = ToolTimeouts{Default: "600000"}
	out, _ = decideOut(t, "go test ./...", o)
	if out.H.U["timeout"] != 600000.0 || !strings.Contains(out.H.Context, "capped at 600s") {
		t.Fatalf("at the cap: %v %q", out.H.U["timeout"], out.H.Context)
	}
}

func TestCdStaysInside(t *testing.T) {
	for dir, want := range map[string]bool{
		"web": true, "web/app": true, "./web": true, ".": true, "/work/wt-api": true, "/work/wt-api/web": true,
		"": false, "..": false, "../x": false, "web/../..": false, "~": false, "~/x": false, "-": false,
		"/": false, "/work": false, "/work/wt-api-evil": false, "/work/wt-api/../x": false,
		".[.]": false, ".?": false, "web/*": false, "/work/wt-api/.[.]": false,
	} {
		if got := cdStaysInside(dir, "/work/wt-api"); got != want {
			t.Errorf("cd %q from /work/wt-api: %v, want %v", dir, got, want)
		}
	}
	if cdStaysInside("/work/wt-api", "") {
		t.Error("an absolute cd with no known cwd must not count as inside")
	}
}

// A real bash with globskipdots off (the default before 5.2) expands `.[.]`
// to "..", so a glob in the cd text can leave cwd.
func TestCdGlobLeavesCwdInRealBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	root := t.TempDir()
	cwd := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "-c", "shopt -u globskipdots 2>/dev/null; cd .[.]/.[.] && pwd -P")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	want, _ := filepath.EvalSymlinks(root)
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("cd .[.]/.[.] from %s landed in %s, want %s", cwd, got, want)
	}
	if cdStaysInside(".[.]/.[.]", cwd) {
		t.Fatal("a cd that bash expands out of cwd must not count as inside")
	}
}

// --cache stays when every typed assignment is in cache.env (the key sees it).
func TestDecideCacheKeptForCacheEnvAssignments(t *testing.T) {
	o := testOptions()
	o.Rules = []config.HookRule{{Match: []string{"npm test"}, Priority: 4, Cache: true}}
	o.CacheEnv = []string{"CI", "NODE_ENV"}
	if got := rewritten(t, "CI=1 NODE_ENV=test npm test", o); !strings.Contains(got, " --cache ") {
		t.Errorf("cache.env assignments keep --cache: %q", got)
	}
	if got := rewritten(t, "CI=1 DEBUG=1 npm test", o); strings.Contains(got, "--cache") {
		t.Errorf("an assignment outside cache.env drops --cache: %q", got)
	}
}

// With the real PATH lookup, a program PATH cannot find is left to the shell
// (it may be an alias or a function there); a path-qualified one is not looked up.
func TestDecideLeavesProgramsPathDoesNotFind(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	o := testOptions()
	o.LookPath = exec.LookPath
	if got := rewritten(t, "go test ./...", o); got == "" {
		t.Error("a program on PATH must be wrapped")
	}
	if got := rewritten(t, "python -m pytest", o); got != "" {
		t.Errorf("python is not on PATH (an alias?), got %q", got)
	}
	if _, why, _ := Decide(hookInput("python -m pytest"), o); !strings.HasPrefix(why, LeftMatched) {
		t.Errorf("why = %q", why)
	}
	if got := rewritten(t, "./gradlew test", o); got == "" {
		t.Error("a path-qualified program is not looked up on PATH")
	}
}
