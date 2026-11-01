package hook

import (
	"encoding/json"
	"strings"
	"testing"
)

type sunk struct{ lane, id string }

// The Claude session id never appears in the rewritten command (that would make
// the command differ in every session and break exact permission rules). It goes
// to the sink, and only when it is a plain tag.
func TestDecideSessionGoesToTheSinkNotTheCommand(t *testing.T) {
	const want = "heavyq 4 --lane wt-api --max-wait 300s -- npm test"
	long := strings.Repeat("x", 80)
	cases := []struct {
		name, session string
		sunk          bool
	}{
		{"uuid", `"3f2a9c1e-77aa-4c1d-b3a0-0123456789ab"`, true},
		{"all allowed characters", `"A.b_c:d-e"`, true},
		{"max length", `"` + long + `"`, true},
		{"absent", ``, false},
		{"empty", `""`, false},
		{"too long", `"` + long + `x"`, false},
		{"space", `"a b"`, false},
		{"command substitution", `"$(touch /tmp/pwned)"`, false},
		{"semicolon", `"a;rm -rf ~"`, false},
		{"path separator", `"../x"`, false},
		{"newline", `"a\nb"`, false},
		{"number", `42`, false},
		{"object", `{"a":1}`, false},
		{"null", `null`, false},
	}
	for _, c := range cases {
		in := `{"cwd":"/work/wt-api","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"npm test"}`
		if c.session != "" {
			in += `,"session_id":` + c.session
		}
		var got []sunk
		o := testOptions()
		o.SessionSink = func(lane, id string) { got = append(got, sunk{lane, id}) }
		out, _, err := Decide([]byte(in+`}`), o)
		if err != nil || out == nil {
			t.Fatalf("%s: a bad session id must never stop the rewrite: out=%s err=%v", c.name, out, err)
		}
		var v struct {
			H struct {
				U map[string]any `json:"updatedInput"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(out, &v); err != nil {
			t.Fatal(err)
		}
		if cmd := v.H.U["command"]; cmd != want {
			t.Errorf("%s: command must not carry the session:\n got  %q\n want %q", c.name, cmd, want)
		}
		switch {
		case c.sunk && len(got) != 1:
			t.Errorf("%s: sink called %d times, want once", c.name, len(got))
		case c.sunk && (got[0].lane != "wt-api" || got[0].id != strings.Trim(c.session, `"`)):
			t.Errorf("%s: sink got %+v", c.name, got)
		case !c.sunk && len(got) != 0:
			t.Errorf("%s: invalid id reached the sink: %+v", c.name, got)
		}
	}
}

// Commands the hook leaves alone must not record a session either.
func TestDecideSinkNotCalledWhenNothingIsWrapped(t *testing.T) {
	var got []sunk
	o := testOptions()
	o.SessionSink = func(lane, id string) { got = append(got, sunk{lane, id}) }
	for _, cmd := range []string{"ls -la", "heavyq 4 -- go test ./...", "go test ./... $(whoami)"} {
		if out, _, _ := Decide(hookInput(cmd), o); out != nil {
			t.Fatalf("%q was rewritten", cmd)
		}
	}
	if len(got) != 0 {
		t.Fatalf("sink called for unwrapped commands: %+v", got)
	}
}
