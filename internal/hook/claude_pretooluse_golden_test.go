package hook

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestDecideGolden(t *testing.T) {
	cases := map[string]struct {
		in   string
		mod  func(*Options)
		file string
	}{
		"default": {in: `{"session_id":"s1","cwd":"/work/wt-api","hook_event_name":"PreToolUse","tool_name":"Bash",` +
			`"tool_input":{"command":"go test ./... 2>&1 | tail -30","description":"Run tests","timeout":200000,"run_in_background":false},"tool_use_id":"t1"}`},
		"allow_light": {in: `{"cwd":"/work/wt-web","hook_event_name":"PreToolUse","tool_name":"Bash",` +
			`"tool_input":{"command":"cd web && npm run lint","description":"Lint"}}`,
			mod: func(o *Options) { o.Permission = "allow"; o.MaxWait = 90 * time.Second }},
	}
	for name, c := range cases {
		o := testOptions()
		if c.mod != nil {
			c.mod(&o)
		}
		out, _, err := Decide([]byte(c.in), o)
		if err != nil || out == nil {
			t.Fatalf("%s: out=%s err=%v", name, out, err)
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, out, "", "  "); err != nil {
			t.Fatal(err)
		}
		got := buf.Bytes()
		path := filepath.Join("testdata", name+".golden.json")
		if *update {
			if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./internal/hook -update)", err)
		}
		if strings.TrimSpace(string(want)) != string(got) {
			t.Errorf("%s: golden mismatch\n got  %s\n want %s", name, got, want)
		}
	}
}
