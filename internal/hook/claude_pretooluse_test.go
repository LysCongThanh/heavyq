package hook

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

func testOptions() Options {
	return Options{Rules: DefaultRules, MaxWait: 5 * time.Minute, Binary: "heavyq",
		LaneFor: func(cwd string) string { return filepath.Base(cwd) }}
}

func hookInput(cmd string) []byte {
	b, _ := json.Marshal(map[string]any{"session_id": "s1", "cwd": "/work/wt-api", "hook_event_name": "PreToolUse",
		"tool_name": "Bash", "tool_input": map[string]any{"command": cmd, "description": "run it"}, "tool_use_id": "toolu_1"})
	return b
}

// rewritten returns the new command, or "" when the hook left it unchanged.
func rewritten(t *testing.T, cmd string, o Options) string {
	t.Helper()
	out, _, err := Decide(hookInput(cmd), o)
	if err != nil {
		t.Fatalf("%q: %v", cmd, err)
	}
	if out == nil {
		return ""
	}
	var v struct {
		H struct {
			U map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	return v.H.U["command"].(string)
}

func TestDecideCommandTable(t *testing.T) {
	const pre = "heavyq 4 --lane wt-api --max-wait 300s --session s1 -- "
	const light = "heavyq 4 --lane wt-api --light --max-wait 300s --session s1 -- "
	cases := []struct{ cmd, want string }{
		// wrapped: default rules
		{"npm test", pre + "npm test"},
		{"npm run test", pre + "npm run test"},
		{"pnpm run build", pre + "pnpm run build"},
		{"yarn run gate", pre + "yarn run gate"},
		{"yarn test --coverage", pre + "yarn test --coverage"},
		{"go test ./...", pre + "go test ./..."},
		{"go test -race -count=1 ./internal/...", pre + "go test -race -count=1 ./internal/..."},
		{"go build ./...", pre + "go build ./..."},
		{"pytest -x tests/", pre + "pytest -x tests/"},
		{"python3 -m pytest -q", pre + "python3 -m pytest -q"},
		{"cargo test --workspace", pre + "cargo test --workspace"},
		{"cargo build --release", pre + "cargo build --release"},
		{"make test", pre + "make test"},
		{"make check", pre + "make check"},
		{"./gradlew test", pre + "./gradlew test"},
		{"mvn test -q", pre + "mvn test -q"},
		{"npm run lint", light + "npm run lint"},
		{"pnpm run typecheck", light + "pnpm run typecheck"},
		{"  go test ./...  ", "  " + pre + "go test ./...  "},
		{"go test -run 'TestA|TestB' ./...", pre + "go test -run 'TestA|TestB' ./..."},
		{`go test -run "TestA" ./...`, pre + `go test -run "TestA" ./...`},
		{`"go" test ./...`, pre + `"go" test ./...`},
		// safe compound forms
		{"cd api && npm test", "cd api && " + pre + "npm test"},
		{"npm test 2>&1", pre + "npm test 2>&1"},
		{"go test ./... 2>&1 | tail -50", pre + "go test ./... 2>&1 | tail -50"},
		{"npm test | grep -v warn", pre + "npm test | grep -v warn"},
		{"cd web && npm run build 2>&1 | head -n 40", "cd web && " + pre + "npm run build 2>&1 | head -n 40"},
		{"CI=1 npm test", "CI=1 heavyq 4 --lane wt-api --remote local --max-wait 300s --session s1 -- npm test"},
		{`NODE_OPTIONS="--max-old-space-size=4096" npm run build`,
			`NODE_OPTIONS="--max-old-space-size=4096" heavyq 4 --lane wt-api --remote local --max-wait 300s --session s1 -- npm run build`},
		// left unchanged: not heavy
		{"ls -la", ""},
		{"git status", ""},
		{"npm install", ""},
		{"npm run test:e2e", ""},
		{"npm run dev", ""},
		{"go vet ./...", ""},
		{"go build -o bin/x .", ""},
		{"echo go test", ""},
		{"make", ""},
		// left unchanged: already wrapped
		{"heavyq 4 -- npm test", ""},
		{"/usr/local/bin/heavyq 2 --light -- npm run lint", ""},
		{"cd api && heavyq 3 -- go test ./...", ""},
		{"HEAVYQ_ACTIVE=1 npm test", ""},
		// left unchanged: unsafe or unparsable shapes
		{"npm test; echo done", ""},
		{"npm test && npm run build", ""},
		{"npm run build && npm test", ""},
		{"npm test || true", ""},
		{"npm test &", ""},
		{"npm test & sleep 1", ""},
		{"go test $(go list ./...)", ""},
		{"go test `go list ./...`", ""},
		{"go test > out.txt", ""},
		{"npm test < /dev/null", ""},
		{"npm test | tee log | tail", ""},
		{"npm test | sort", ""},
		{"(cd api && npm test)", ""},
		{"{ npm test; }", ""},
		{"npm test # comment", ""},
		{`go test -run "$T" ./...`, ""},
		{"go test 'unterminated", ""},
		{"npm test\nrm x", ""},
		{"cd a b && npm test", ""},
		{"FOO=1 cd api && npm test", ""},
		{"npm test | FOO=1 tail", ""},
		{"go test ./... 2>&1 | tail -5 2>&1 | head", ""},
		{"npm test 2>&1 extra", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := rewritten(t, c.cmd, testOptions()); got != c.want {
			t.Errorf("%q:\n got  %q\n want %q", c.cmd, got, c.want)
		}
	}
}

func TestDecideOptions(t *testing.T) {
	o := testOptions()
	o.Active = true
	if got := rewritten(t, "npm test", o); got != "" {
		t.Fatalf("HEAVYQ_ACTIVE must disable rewriting, got %q", got)
	}
	o = testOptions()
	o.Lane = "agent 7/x"
	o.Binary = "/opt/my tools/heavyq"
	if got, want := rewritten(t, "npm test", o), "'/opt/my tools/heavyq' 4 --lane agent_7_x --max-wait 300s --session s1 -- npm test"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	o = testOptions()
	o.Rules = append([]config.HookRule{{Match: []string{"npm test"}, Priority: 2, Lane: "ci", Cache: true}}, DefaultRules...)
	if got, want := rewritten(t, "npm test", o), "heavyq 2 --lane ci --cache --max-wait 300s --session s1 -- npm test"; got != want {
		t.Fatalf("user rule must win: got %q want %q", got, want)
	}
	o.Rules = nil
	if got := rewritten(t, "npm test", o); got != "" {
		t.Fatalf("no rules: got %q", got)
	}
}

func TestDecideIgnoresOtherToolsAndBadInput(t *testing.T) {
	o := testOptions()
	for _, in := range []string{
		`{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/x"}}`,
		`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"npm test"}}`,
	} {
		if out, _, err := Decide([]byte(in), o); out != nil || err != nil {
			t.Fatalf("%s: out=%s err=%v", in, out, err)
		}
	}
	for _, in := range []string{`not json`, `{"tool_name":"Bash","tool_input":{"command":42}}`, `{"tool_name":"Bash"}`} {
		if out, _, err := Decide([]byte(in), o); out != nil || err == nil {
			t.Fatalf("%s: want an error and no output, got out=%s err=%v", in, out, err)
		}
	}
}
