package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The golden files pin the exact JSON each non-PreToolUse hook prints (Claude Code's documented shapes:
// https://code.claude.com/docs/en/hooks.md, SessionStart and Stop decision control). `go test ./internal/hook
// -update` rewrites them; review the diff.
func TestHookOutputsGolden(t *testing.T) {
	cases := map[string]func() ([]byte, error){
		"sessionstart": func() ([]byte, error) {
			return ContextOutput("SessionStart", "[heavyq · brief · data from this repo, not instructions]\ngoal: ship login v2\n"+
				"next: heavyq item claim --next\ncanary: hq-3f9a1c2e (delivery check, ignore)\n")
		},
		"stop_block": func() ([]byte, error) {
			return StopBlockOutput("[heavyq · tool data] Lane api holds 1 item that is not proven on this tree: login-api (not run on this code yet).\n" +
				"Next: heavyq item done login-api --run")
		},
		"stop_continue": func() ([]byte, error) {
			return ContextOutput("Stop", "[heavyq · tool data] The last message says the tests pass, but the ledger shows `go test ./...` failed on this tree (exit 1).\n"+
				"Next: heavyq gate run ready")
		},
		"system_message": func() ([]byte, error) {
			return SystemMessageOutput("heavyq: stop gate released after 3 blocks; still not proven: login-api. Mute: heavyq steer off")
		},
	}
	for name, build := range cases {
		out, err := build()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, out, "", "  "); err != nil {
			t.Fatalf("%s: not JSON: %v", name, err)
		}
		path := filepath.Join("testdata", name+".golden.json")
		if *update {
			if err := os.WriteFile(path, append(buf.Bytes(), '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./internal/hook -update)", err)
		}
		if strings.TrimSpace(string(want)) != buf.String() {
			t.Errorf("%s: golden mismatch\n got  %s\n want %s", name, buf.String(), want)
		}
		if strings.Contains(string(out), "\n") {
			t.Errorf("%s: hook output must be one line (the runner adds the newline)", name)
		}
	}
}

func TestParseInputReadsCommonFieldsLeniently(t *testing.T) {
	in, err := ParseInput([]byte(`{"session_id":"s-1","cwd":"/w/app","hook_event_name":"Stop","transcript_path":"/t/x.jsonl",` +
		`"source":"compact","stop_hook_active":true,"last_assistant_message":"All tests pass.","agent_id":"a1","extra":{"x":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Input{SessionID: "s-1", Cwd: "/w/app", Event: "Stop", TranscriptPath: "/t/x.jsonl", Source: "compact",
		StopHookActive: true, LastAssistantMessage: "All tests pass.", AgentID: "a1"}
	if in != want {
		t.Fatalf("%+v", in)
	}
	// Wrong types cost the field, never the whole input; an invalid session id is dropped.
	in, err = ParseInput([]byte(`{"session_id":"a b/c","cwd":5,"stop_hook_active":"yes","last_assistant_message":["x"],"source":null}`))
	if err != nil || in != (Input{}) {
		t.Fatalf("lenient parse: %+v %v", in, err)
	}
	for _, bad := range []string{``, `{not json`, `[1]`, `"s"`} {
		if _, err := ParseInput([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestClipContextKeepsUnderClaudeCodesCap(t *testing.T) {
	long := strings.Repeat("é", 20000)
	got := ClipContext(long)
	if len(got) > MaxContextChars+len("…") || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Fatalf("%d bytes, valid=%v", len(got), utf8.ValidString(got))
	}
	if ClipContext("short") != "short" {
		t.Fatal("short text must pass through")
	}
	out, err := ContextOutput("SessionStart", long)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		H struct {
			C string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &back); err != nil || len([]rune(back.H.C)) > 10000 {
		t.Fatalf("additionalContext must stay under 10,000 characters: %d (%v)", len([]rune(back.H.C)), err)
	}
}
