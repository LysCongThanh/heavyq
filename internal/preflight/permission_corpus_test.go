package preflight

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpusFile is testdata/permission-corpus/corpus.json: commands with the decision
// a live headless Claude Code made for them under given rules, recorded by
// testdata/permission-corpus/record.py (`claude -p --permission-prompts none`).
type corpusFile struct {
	ClaudeCodeVersion string `json:"claude_code_version"`
	Recorded          string `json:"recorded"`
	Source            string `json:"source"` // "recorded", or "doc-derived" when built from the documentation
	Rulesets          []struct {
		Name        string `json:"name"`
		Hook        string `json:"hook"` // "", "none", "rewrite" (heavyq hook, no decision) or "rewrite-allow" (hook.permission = allow)
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
			Ask   []string `json:"ask"`
		} `json:"permissions"`
		Cases []struct {
			Command  string `json:"command"`
			Decision string `json:"decision"`           // "allow": it ran. "refuse": it was denied (deny rule, ask rule or no allow rule)
			Reason   string `json:"reason"`             // Claude Code's decision_reason_type, for the record
			MayWarn  string `json:"may_warn,omitempty"` // why the matcher can only warn here
		} `json:"cases"`
	} `json:"rulesets"`
}

// verdictOf runs the permission check on one command and names what it said:
// "ok", "warn" (may prompt, not sure) or "block" (NO-GO).
func verdictOf(t *testing.T, cmd, settings string, hook Rewriter) string {
	t.Helper()
	out := CheckPermissions(PermInput{Commands: []string{cmd}, Rules: rulesOf(t, settings), Rewrite: hook, Settings: ".claude/settings.json"})
	got := "ok"
	for _, f := range out {
		switch {
		case f.Severity == Block:
			return "block"
		case f.Subject != "headless":
			got = "warn"
		}
	}
	return got
}

// TestPermissionMatcherCorpus: the matcher agrees with every decision Claude Code
// made. A command that ran must not block, a command that was refused must not
// pass; the shapes the matcher cannot decide (may_warn in the corpus, with the
// reason) must warn, and only those.
func TestPermissionMatcherCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "permission-corpus", "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus corpusFile
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.ClaudeCodeVersion == "" || corpus.Recorded == "" || (corpus.Source != "recorded" && corpus.Source != "doc-derived") {
		t.Fatalf("the corpus must say which Claude Code version, when and how it was made: %q %q %q", corpus.ClaudeCodeVersion, corpus.Recorded, corpus.Source)
	}
	total := 0
	for _, rs := range corpus.Rulesets {
		settings, err := json.Marshal(map[string]any{"permissions": rs.Permissions})
		if err != nil {
			t.Fatal(err)
		}
		var hook Rewriter
		switch rs.Hook {
		case "", "none":
		case "rewrite":
			hook = fakeHook(false)
		case "rewrite-allow":
			hook = fakeHook(true)
		default:
			t.Fatalf("ruleset %s: unknown hook mode %q", rs.Name, rs.Hook)
		}
		for _, c := range rs.Cases {
			total++
			t.Run(rs.Name+"/"+c.Command, func(t *testing.T) {
				want := map[string]string{"allow": "ok", "refuse": "block"}[c.Decision]
				if want == "" {
					t.Fatalf("decision %q is neither allow nor refuse", c.Decision)
				}
				if c.MayWarn != "" {
					want = "warn"
				}
				if got := verdictOf(t, c.Command, string(settings), hook); got != want {
					t.Errorf("Claude Code %s %q under %s, the matcher says %s, want %s%s\n  rules: %s",
						map[string]string{"allow": "ran", "refuse": "refused"}[c.Decision], c.Command, rs.Name, got, want,
						map[bool]string{true: " (may_warn: " + c.MayWarn + ")", false: ""}[c.MayWarn != ""], settings)
				}
			})
		}
	}
	if total < 40 {
		t.Errorf("the corpus holds %d cases; a corpus this small proves little", total)
	}
}

// TestPermissionMatcherWrappersFromDocs covers wrapper stripping the corpus does
// not record (documented in code.claude.com/docs/en/permissions, "Wrappers").
func TestPermissionMatcherWrappersFromDocs(t *testing.T) {
	allow := `{"permissions":{"allow":["Bash(go test:*)"],"deny":["Bash(rm:*)"]}}`
	for cmd, want := range map[string]string{
		"stdbuf -oL go test ./...":      "ok",
		"command go test ./...":         "ok",
		"timeout -s KILL 30 go test":    "ok",
		"watch go test ./...":           "block", // an exec wrapper that is not stripped
		"FOO=bar rm -rf x":              "block", // a deny rule matches past any assignment
		"nohup rm -rf x":                "block",
		"git -C . push":                 "block",
		"gofmt -l . | (! grep .)":       "block", // gofmt has no rule here
		"go test ./... && echo done":    "ok",
		"go test ./... | tee out.log":   "block",
		"go test ./... > out.txt":       "block",
		"echo \"a > b\" && go test ./.": "ok", // a quoted > is not a redirection
	} {
		if got := verdictOf(t, cmd, allow, nil); got != want {
			t.Errorf("%q: %s, want %s", cmd, got, want)
		}
	}
	if got := verdictOf(t, "go test ./... > out.txt", `{"permissions":{"allow":["Bash(go test:*)","Edit(./out.txt)"]}}`, nil); got != "warn" {
		t.Errorf("an Edit rule decides a redirection to a file, which is not modelled: %s", got)
	}
	if !strings.Contains(strings.Join(shellFields(`echo "a  b" c`), "|"), `"a  b"|c`) {
		t.Error("quotes keep a word together")
	}
}
