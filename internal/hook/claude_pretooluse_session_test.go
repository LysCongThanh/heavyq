package hook

import (
	"encoding/json"
	"strings"
	"testing"
)

// The Claude session id reaches the ledger as --session, but only when it is a
// plain tag: the value lands inside a shell command line.
func TestDecideSessionFlag(t *testing.T) {
	const base = "heavyq 4 --lane wt-api --max-wait 300s"
	without := base + " -- npm test"
	with := func(id string) string { return base + " --session " + id + " -- npm test" }
	cases := []struct {
		name, session, want string
	}{
		{"uuid", `"3f2a9c1e-77aa-4c1d-b3a0-0123456789ab"`, with("3f2a9c1e-77aa-4c1d-b3a0-0123456789ab")},
		{"all allowed characters", `"A.b_c:d-e"`, with("A.b_c:d-e")},
		{"max length", `"` + strings.Repeat("x", 80) + `"`, with(strings.Repeat("x", 80))},
		{"absent", ``, without},
		{"empty", `""`, without},
		{"too long", `"` + strings.Repeat("x", 81) + `"`, without},
		{"space", `"a b"`, without},
		{"command substitution", `"$(touch /tmp/pwned)"`, without},
		{"semicolon", `"a;rm -rf ~"`, without},
		{"quote", `"a'b"`, without},
		{"newline", `"a\nb"`, without},
		{"number", `42`, without},
		{"object", `{"a":1}`, without},
		{"null", `null`, without},
	}
	for _, c := range cases {
		in := `{"cwd":"/work/wt-api","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"npm test"}`
		if c.session != "" {
			in += `,"session_id":` + c.session
		}
		out, _, err := Decide([]byte(in+`}`), testOptions())
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
		if got := v.H.U["command"]; got != c.want {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
	}
}
