package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePostToolUse(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		skip    string // reason it is not for the mentor
		wantErr bool
		want    PostCall
	}{
		{name: "failure event of a wrapped run",
			in: `{"session_id":"s1","cwd":"/work/wt-api","hook_event_name":"PostToolUseFailure","tool_name":"Bash",` +
				`"tool_input":{"command":"heavyq 4 --lane wt-api --max-wait 300s -- go test ./... 2>&1 | tail -30"},` +
				`"error":"Exit code 1\nFAIL","is_interrupt":false,"duration_ms":4187}`,
			want: PostCall{Event: EventPostToolUseFailure, Session: "s1", Cwd: "/work/wt-api", Lane: "wt-api",
				Command: "heavyq 4 --lane wt-api --max-wait 300s -- go test ./... 2>&1 | tail -30"}},
		{name: "success event, no lane in the command",
			in: `{"session_id":"s2","cwd":"/w","hook_event_name":"PostToolUse","tool_name":"Bash",` +
				`"tool_input":{"command":"go test ./..."},"tool_response":{"stdout":"ok"}}`,
			want: PostCall{Event: EventPostToolUse, Session: "s2", Cwd: "/w", Command: "go test ./..."}},
		{name: "the lane flag belongs to heavyq, not to the wrapped command",
			in:   `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"heavyq 4 -- mytool --lane x"}}`,
			want: PostCall{Event: EventPostToolUse, Command: "heavyq 4 -- mytool --lane x"}},
		{name: "unsafe session id is dropped, the call stays",
			in:   `{"session_id":"a b;c","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`,
			want: PostCall{Event: EventPostToolUse, Command: "ls"}},
		{name: "a command that is not a string is no command",
			in:   `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":5}}`,
			want: PostCall{Event: EventPostToolUse}},
		{name: "another tool", in: `{"hook_event_name":"PostToolUse","tool_name":"Edit","tool_input":{}}`, skip: "tool Edit"},
		{name: "another event", in: `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`, skip: "event PreToolUse"},
		{name: "no event", in: `{"tool_name":"Bash"}`, skip: "event "},
		{name: "not json", in: `{nope`, wantErr: true},
	}
	for _, c := range cases {
		got, skip, err := ParsePostToolUse([]byte(c.in))
		switch {
		case c.wantErr:
			if err == nil {
				t.Errorf("%s: want an error", c.name)
			}
		case err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.skip != "":
			if got != nil || skip != c.skip {
				t.Errorf("%s: got %+v skip %q, want skip %q", c.name, got, skip, c.skip)
			}
		case got == nil || *got != c.want:
			t.Errorf("%s:\n got  %+v\n want %+v", c.name, got, c.want)
		}
	}
}

func TestPostContextGolden(t *testing.T) {
	text := "[heavyq · tool data] `go test ./...` exited 1 3 times in a row on the same tree (0123456789ab). " +
		"Next: change the code or its inputs before rerunning, or report BLOCKED with needs:."
	for _, event := range []string{EventPostToolUse, EventPostToolUseFailure} {
		out, err := PostContext(event, text)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, out, "", "  "); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("testdata", "mentor_"+strings.ToLower(event)+".golden.json")
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
			t.Errorf("%s: golden mismatch\n got  %s\n want %s", event, buf.String(), want)
		}
	}
	if _, err := PostContext("PreToolUse", "x"); err == nil {
		t.Fatal("only post-tool events may carry additionalContext from the mentor")
	}
}
