package preflight

import (
	"reflect"
	"strings"
	"testing"
)

func rulesOf(t *testing.T, settings string) Rules {
	t.Helper()
	var r Rules
	if err := r.Merge([]byte(settings)); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMatchRule(t *testing.T) {
	cases := []struct {
		spec, cmd string
		want      bool
	}{
		{"go test:*", "go test ./...", true},
		{"go test:*", "go test", true},
		{"go test:*", "go testing", false},
		{"go test:*", "go build", false},
		{"go test *", "go test ./a", true},
		{"go test *", "go test", false},
		{"npm run build", "npm run build", true},
		{"npm run build", "npm run build --x", false},
		{"git * main", "git push origin main", true},
		{"git * main", "git push origin dev", false},
		{"", "anything", true},
		{"a.b*", "a.bc", true},
		{"a.b*", "axb", false}, // the dot is a dot, not a regexp
	}
	for _, c := range cases {
		if got := matchRule(c.spec, c.cmd); got != c.want {
			t.Errorf("matchRule(%q, %q) = %v, want %v", c.spec, c.cmd, got, c.want)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		in     string
		want   []string
		simple bool
	}{
		{"go test ./...", []string{"go test ./..."}, true},
		{"cd x && go test ./... 2>&1 | tail -5", []string{"cd x", "go test ./...", "tail -5"}, true},
		{"gofmt -l . | (! grep .)", []string{"gofmt -l .", "grep ."}, true},
		{"echo 'a && b' ; ls", []string{"echo 'a && b'", "ls"}, true},
		{`echo "a | b"`, []string{`echo "a | b"`}, true},
		{"go test >/dev/null 2>&1", []string{"go test"}, true},
		{"go test $(cat x)", nil, false},
		{"echo `date`", nil, false},
		{`echo "$(date)"`, nil, false},
		{"echo 'unterminated", nil, false},
		{"a\nb", []string{"a", "b"}, true},
	}
	for _, c := range cases {
		got, simple := splitCommand(c.in)
		if simple != c.simple || (simple && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("splitCommand(%q) = %q, %v; want %q, %v", c.in, got, simple, c.want, c.simple)
		}
	}
}

func TestMergeRules(t *testing.T) {
	r := rulesOf(t, `{"permissions":{"allow":["Bash(go test:*)","Read(*)","Bash(npm run build)"],"deny":["Bash(rm:*)"],"ask":["Bash(git push:*)"]},
		"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/usr/bin/heavyq hook claude-pretooluse"}]}]}}`)
	if !reflect.DeepEqual(r.Allow, []string{"go test:*", "npm run build"}) || !reflect.DeepEqual(r.Deny, []string{"rm:*"}) ||
		!reflect.DeepEqual(r.Ask, []string{"git push:*"}) || !r.HookInstalled || r.Bypass {
		t.Errorf("%+v", r)
	}
	if !rulesOf(t, `{"permissions":{"defaultMode":"bypassPermissions"}}`).Bypass ||
		!rulesOf(t, `{"permissions":{"allow":["Bash"]}}`).Bypass ||
		!rulesOf(t, `{"permissions":{"allow":["Bash(*)"]}}`).Bypass {
		t.Error("bypass modes and blanket allow rules switch the check off")
	}
	if !rulesOf(t, `{"enabledPlugins":{"heavyq@heavyq":true}}`).HookInstalled {
		t.Error("the plugin ships the hook")
	}
	if rulesOf(t, `{"hooks":"weird","permissions":{"allow":["Bash(ls:*)"]}}`).Allow[0] != "ls:*" {
		t.Error("a malformed hooks block must not cost the rules")
	}
	var bad Rules
	if bad.Merge([]byte("{not json")) == nil {
		t.Error("invalid JSON is an error the caller reports")
	}
}

// fakeHook mimics the PreToolUse hook for `go test`: priority 4, lane "api".
func fakeHook(approved bool) Rewriter {
	return func(cmd string) (string, bool, bool) {
		if strings.HasPrefix(cmd, "go test") {
			return "heavyq 4 --lane api --max-wait 300s -- " + cmd, approved, true
		}
		return cmd, false, false
	}
}

func TestCheckPermissions(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		hook     Rewriter
		cmds     []string
		want     []string
	}{
		{"covered by allow rules", `{"permissions":{"allow":["Bash(go test:*)","Bash(npm run build)"]}}`, nil,
			[]string{"go test ./...", "npm run build"}, nil},
		{"not covered", `{"permissions":{"allow":["Bash(go test:*)"]}}`, nil,
			[]string{"go test ./...", "npm run lint"}, []string{"block:`npm run lint` may prompt: no allow rule covers it", "warn:unattended"}},
		{"read-only commands never prompt", `{}`, nil, []string{"ls -la", "git status", "cd x && cat y | head"}, nil},
		{"git push is not read-only", `{}`, nil, []string{"git push"}, []string{"block:may prompt", "warn:unattended"}},
		{"every part of a compound command must be covered", `{"permissions":{"allow":["Bash(go vet:*)"]}}`, nil,
			[]string{"go vet ./... && go test ./..."}, []string{"block:may prompt", "warn:unattended"}},
		{"gate pipeline", `{"permissions":{"allow":["Bash(gofmt:*)"]}}`, nil, []string{"gofmt -l . | (! grep .)"}, nil},
		{"substitution is too complex to match", `{"permissions":{"allow":["Bash(echo:*)"]}}`, nil,
			[]string{"echo $(date)"}, []string{"block:too complex", "warn:unattended"}},
		{"deny wins over allow", `{"permissions":{"allow":["Bash(go test:*)"],"deny":["Bash(go test:*)"]}}`, nil,
			[]string{"go test ./..."}, []string{"block:denied by permissions.deny", "warn:unattended"}},
		{"ask rule prompts", `{"permissions":{"allow":["Bash(git push:*)"],"ask":["Bash(git push:*)"]}}`, nil,
			[]string{"git push"}, []string{"block:an ask rule matches", "warn:unattended"}},
		{"bypass mode", `{"permissions":{"defaultMode":"bypassPermissions"}}`, nil, []string{"rm -rf x"}, nil},
		{"the hook rewrites it, the rule no longer matches", `{"permissions":{"allow":["Bash(go test:*)"]}}`, fakeHook(false),
			[]string{"go test ./..."}, []string{"block:the heavyq hook rewrites it to `heavyq 4 --lane api", "warn:unattended"}},
		{"the rewritten prefix is allowed", `{"permissions":{"allow":["Bash(heavyq 4 --lane api --max-wait 300s -- go test *)"]}}`, fakeHook(false),
			[]string{"go test ./..."}, nil},
		{"hook.permission allow approves the rewrite", `{}`, fakeHook(true), []string{"go test ./..."}, nil},
		{"hook approval does not beat deny", `{"permissions":{"deny":["Bash(heavyq:*)"]}}`, fakeHook(true),
			[]string{"go test ./..."}, []string{"block:denied", "warn:unattended"}},
		{"duplicates are judged once", `{}`, nil, []string{"make x", "make x"}, []string{"block:`make x` may prompt", "warn:unattended"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckPermissions(PermInput{Commands: c.cmds, Rules: rulesOf(t, c.settings), Rewrite: c.hook, Settings: ".claude/settings.local.json"})
			assertFindings(t, got, c.want)
		})
	}
}

func TestPermissionFixesAreRules(t *testing.T) {
	fs := CheckPermissions(PermInput{Commands: []string{"go test ./...", "npm run build"}, Settings: "s.json"})
	if fs[0].Fix != `edit s.json: add "Bash(go test:*)" to permissions.allow` ||
		fs[1].Fix != `edit s.json: add "Bash(npm run:*)" to permissions.allow` {
		t.Errorf("%+v", fs)
	}
	fs = CheckPermissions(PermInput{Commands: []string{"make -j4 test", "pytest -x"}, Settings: "s.json"})
	if fs[0].Fix != `edit s.json: add "Bash(make:*)" to permissions.allow` || fs[1].Fix != `edit s.json: add "Bash(pytest:*)" to permissions.allow` {
		t.Errorf("a flag is not part of a rule: %+v", fs)
	}
	fs = CheckPermissions(PermInput{Commands: []string{"sh -c 'make x'", "env A=1 make"}, Settings: "s.json"})
	if fs[0].Fix != `edit s.json: add "Bash(sh -c 'make x')" to permissions.allow` || !strings.Contains(fs[1].Fix, `"Bash(env A=1 make)"`) {
		t.Errorf("a wrapper is allowed only as the exact command: %+v", fs)
	}
	fs = CheckPermissions(PermInput{Commands: []string{"go test ./..."}, Rewrite: fakeHook(false), Settings: "s.json"})
	if !strings.Contains(fs[0].Fix, `Bash(heavyq 4 --lane api --max-wait 300s -- go test *)`) || strings.Contains(fs[0].Fix, "heavyq:*") {
		t.Errorf("a rewritten command is allowed by its exact prefix, never by a blanket heavyq rule: %+v", fs)
	}
}

func TestPermissionFindingsAreCappedAndRedacted(t *testing.T) {
	var cmds []string
	for _, w := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"} {
		cmds = append(cmds, "tool-"+w+" run")
	}
	fs := CheckPermissions(PermInput{Commands: cmds, Settings: "s"})
	if len(fs) != maxPromptFindings+2 || !strings.Contains(fs[maxPromptFindings].Text, "3 more commands may prompt") {
		t.Fatalf("%d findings, last: %+v", len(fs), fs[len(fs)-2:])
	}
	fs = CheckPermissions(PermInput{Commands: []string{"deploy --token=sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}, Settings: "s"})
	if strings.Contains(fs[0].Text, "abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("secret leaked: %s", fs[0].Text)
	}
}
