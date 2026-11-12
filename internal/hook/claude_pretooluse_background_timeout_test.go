package hook

import (
	"encoding/json"
	"testing"
	"time"
)

// A background call has Claude Code's background limits (default about 30
// min, at least the foreground cap, 2 h by default), which the hook cannot
// read: it never applies the foreground default or cap to one.
func TestToolTimeoutBackgroundSkipsForegroundDefaultAndCap(t *testing.T) {
	const wait = 5 * time.Minute
	for _, c := range []struct {
		raw         string
		env         ToolTimeouts
		want        int64
		explanation string
	}{
		{"", ToolTimeouts{}, 0, "no timeout: left to Claude Code's background default"},
		{`"x"`, ToolTimeouts{}, 0, "a non-number timeout is left as it was"},
		{"3600000", ToolTimeouts{}, 3900000, "over the foreground cap: timeout plus wait"},
		{"200000", ToolTimeouts{Max: "100000"}, 500000, "BASH_MAX_TIMEOUT_MS is not the background cap"},
		{"1e300", ToolTimeouts{}, maxTimerMS, "an absurd timeout does not overflow"},
	} {
		var raw json.RawMessage
		if c.raw != "" {
			raw = json.RawMessage(c.raw)
		}
		if ms, _, capped := toolTimeout(raw, true, wait, c.env); ms != c.want || capped {
			t.Errorf("%s: got %d/%v, want %d/false", c.explanation, ms, capped, c.want)
		}
	}
}

// decideInput runs Decide on a Bash call with the given tool_input fields.
func decideInput(t *testing.T, input map[string]any, o Options) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"session_id": "s1", "cwd": "/work/wt-api", "hook_event_name": "PreToolUse",
		"tool_name": "Bash", "tool_input": input, "tool_use_id": "toolu_1"})
	res, why, err := Decide(b, o)
	if err != nil || res == nil {
		t.Fatalf("%v: not rewritten: why=%q err=%v", input, why, err)
	}
	var out struct {
		H struct {
			U map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatal(err)
	}
	return out.H.U
}

func TestDecideBackgroundTimeout(t *testing.T) {
	o := testOptions()
	u := decideInput(t, map[string]any{"command": "go test ./...", "timeout": 3600000, "run_in_background": true}, o)
	if u["timeout"] != 3900000.0 || u["run_in_background"] != true {
		t.Errorf("background with timeout: %v", u)
	}
	u = decideInput(t, map[string]any{"command": "go test ./...", "run_in_background": true}, o)
	if _, ok := u["timeout"]; ok || u["run_in_background"] != true {
		t.Errorf("background without timeout must leave timeout out: %v", u)
	}
	u = decideInput(t, map[string]any{"command": "go test ./...", "timeout": 3600000, "run_in_background": false}, o)
	if u["timeout"] != 600000.0 {
		t.Errorf("foreground keeps the cap: %v", u)
	}
	u = decideInput(t, map[string]any{"command": "go test ./...", "run_in_background": "true"}, o)
	if u["timeout"] != 420000.0 {
		t.Errorf("only JSON true is background: %v", u)
	}
}
