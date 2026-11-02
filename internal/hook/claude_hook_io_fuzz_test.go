package hook

import (
	"encoding/json"
	"testing"
)

// FuzzParseInput: the hook JSON comes from another program. Whatever it holds, parsing must not panic, and a
// session id it returns must be a valid tag.
func FuzzParseInput(f *testing.F) {
	for _, s := range []string{
		`{"session_id":"s-1","cwd":"/w","hook_event_name":"Stop","stop_hook_active":true,"last_assistant_message":"All tests pass."}`,
		`{"session_id":["x"],"cwd":5,"stop_hook_active":"yes"}`,
		`{"source":null,"transcript_path":{"a":1}}`,
		`[]`, ``, `{`, `{"session_id":"a b/c"}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		in, err := ParseInput(b)
		if err != nil {
			return
		}
		if in.SessionID != "" && len(in.SessionID) > 80 {
			t.Fatalf("session id too long: %q", in.SessionID)
		}
	})
}

// FuzzMergeEntries: install and uninstall read a settings file the user may have edited by hand. They must not
// panic; when they accept a file, the result is valid JSON, installing twice changes nothing more, and
// uninstalling it leaves no heavyq hook.
func FuzzMergeEntries(f *testing.F) {
	for _, s := range []string{
		``, `{}`, `{"hooks":{}}`, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"x"}]}]}}`,
		`{"hooks":{"Stop":{}}}`, `{"hooks":[]}`, `{"env":{"A":"b"},"hooks":{"PreToolUse":[null,1,"x",{"hooks":3}]}}`,
		`{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"command":"heavyq hook claude-sessionstart"}]}]}}`,
	} {
		f.Add([]byte(s))
	}
	entries := EntriesFor("heavyq", []string{"queue", "brief", "stop"})
	f.Fuzz(func(t *testing.T, b []byte) {
		out, _, err := MergeEntries(b, entries)
		if err != nil {
			return
		}
		if !json.Valid(out) {
			t.Fatalf("install produced invalid JSON:\n%s", out)
		}
		again, changed, err := MergeEntries(out, entries)
		if err != nil || changed {
			t.Fatalf("a second install must be a no-op: changed=%v err=%v\n%s\n---\n%s", changed, err, out, again)
		}
		un, _, err := MergeUninstall(out)
		if err != nil || !json.Valid(un) {
			t.Fatalf("uninstall: %v\n%s", err, un)
		}
		if _, changed, _ := MergeUninstall(un); changed {
			t.Fatalf("a second uninstall must be a no-op:\n%s", un)
		}
	})
}
