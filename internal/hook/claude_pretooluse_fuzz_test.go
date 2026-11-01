package hook

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// fuzzOptions match every default rule plus a few trivial programs, so the
// fuzzer reaches the rewrite path without having to guess "npm test" first.
func fuzzOptions() Options {
	o := testOptions()
	extra := config.HookRule{Match: []string{"echo", "ls", "a", "go", "make"}}
	_ = extra.Normalize() // static, valid patterns
	o.Rules = append([]config.HookRule{extra}, o.Rules...)
	return o
}

// metaChars are the characters the parser treats as shell syntax or quoting.
const metaChars = unsafeChars + `'"`

// updatedCommand extracts the rewritten command from Decide's output.
func updatedCommand(t *testing.T, out []byte) string {
	t.Helper()
	var v struct {
		H struct {
			U map[string]json.RawMessage `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("Decide output is not JSON: %v\n%s", err, out)
	}
	var cmd string
	if err := json.Unmarshal(v.H.U["command"], &cmd); err != nil {
		t.Fatalf("updatedInput.command: %v\n%s", err, out)
	}
	return cmd
}

// FuzzDecideRewrite fuzzes the whole PreToolUse path. A rewritten command
// must keep the original around the insertion, add only plain words (never a
// shell metacharacter or quote), parse again as the original argv behind the
// heavyq prefix, and never be wrapped a second time.
func FuzzDecideRewrite(f *testing.F) {
	for _, s := range fuzzSeedCommands {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, cmd string) {
		if len(cmd) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		if !utf8.ValidString(cmd) {
			t.Skip("a command travels as a JSON string, which is always valid UTF-8")
		}
		o := fuzzOptions()
		out, _, err := Decide(hookInput(cmd), o)
		if err != nil {
			t.Fatalf("Decide(%q): %v", cmd, err)
		}
		if out == nil {
			return
		}
		p, err := Parse(cmd)
		if err != nil {
			t.Fatalf("Decide rewrote %q but Parse refuses it: %v", cmd, err)
		}
		newCmd := updatedCommand(t, out)
		head, tail := cmd[:p.ProgAt], cmd[p.ProgAt:]
		if len(newCmd) < len(cmd) || !strings.HasPrefix(newCmd, head) || !strings.HasSuffix(newCmd, tail) {
			t.Fatalf("rewrite of %q does not wrap around the program: %q", cmd, newCmd)
		}
		added := newCmd[len(head) : len(newCmd)-len(tail)]
		if strings.ContainsAny(added, metaChars) {
			t.Fatalf("rewrite of %q added shell syntax %q", cmd, added)
		}
		for _, c := range metaChars {
			if got, want := strings.Count(newCmd, string(c)), strings.Count(cmd, string(c)); got != want {
				t.Fatalf("rewrite of %q changed the count of %q: %d -> %d (%q)", cmd, c, want, got, newCmd)
			}
		}
		again, err := Parse(newCmd)
		if err != nil {
			t.Fatalf("rewrite of %q does not parse: %v (%q)", cmd, err, newCmd)
		}
		wantWords := append(strings.Fields(added), p.Words...)
		if !slices.Equal(again.Env, p.Env) || !slices.Equal(again.Words, wantWords) {
			t.Fatalf("rewrite of %q re-parses as %q %q, want %q %q", cmd, again.Env, again.Words, p.Env, wantWords)
		}
		if out2, _, err := Decide(hookInput(newCmd), o); err != nil || out2 != nil {
			t.Fatalf("rewritten %q would be wrapped again: out=%s err=%v", newCmd, out2, err)
		}
	})
}

// FuzzDecideStdin feeds arbitrary bytes as the hook's stdin: it may return an
// error or nothing, but never panic, and any output must be valid JSON.
func FuzzDecideStdin(f *testing.F) {
	for _, s := range fuzzSeedCommands {
		f.Add(hookInput(s))
	}
	f.Add([]byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"npm test"}}`))
	f.Add([]byte(`{"tool_name":"Bash","tool_input":{"command":42}}`))
	f.Add([]byte(`{"tool_name":"Bash","session_id":7,"tool_input":{"command":"npm test","timeout":"x"}}`))
	f.Add([]byte(`not json`))
	f.Fuzz(func(t *testing.T, stdin []byte) {
		if len(stdin) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		out, _, err := Decide(stdin, fuzzOptions())
		if err != nil && out != nil {
			t.Fatalf("both output and error for %q", stdin)
		}
		if out != nil && !json.Valid(out) {
			t.Fatalf("invalid JSON output for %q: %s", stdin, out)
		}
	})
}
