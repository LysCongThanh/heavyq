package stance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validRecord = `# Decision record: login-api

## Clean-slate answer
From scratch I would not retry the token refresh at all: issue short-lived tokens and drop the refresh path.

## Options
| # | Option | Value | Cost |
|---|---|---|---|
| 1 | Status quo: keep retrying refresh | none | 0 |
| 2 | Incremental: back off the retries | fewer 401s | S |
| 3 | Radical: short-lived tokens, no refresh | no refresh bugs | M |

## Constraints
- Tokens must expire within an hour: REAL (security policy).
- Refresh lives in one module: INERTIA (it was written that way first).

## Choice and migration
Option 3, measured by 401 rate. Migrate behind a flag, remove the refresh module after one release.
`

func TestDecisionRecordValid(t *testing.T) {
	if p := DecisionProblems(validRecord); len(p) != 0 {
		t.Fatalf("valid record has problems: %v", p)
	}
	// Separate headings, other levels, CRLF and a star as the radical mark all pass.
	alt := strings.NewReplacer("## Choice and migration", "### Chosen\nOption 3.\n\n### Migration", "Radical:", "★", "\n", "\r\n").Replace(validRecord)
	if p := DecisionProblems(alt); len(p) != 0 {
		t.Fatalf("alternative layout has problems: %v", p)
	}
}

func TestDecisionRecordProblems(t *testing.T) {
	cases := []struct {
		name   string
		doc    string
		expect string // substring of one problem
	}{
		{"empty file", "", "missing section: clean-slate answer"},
		{"no clean-slate", strings.Replace(validRecord, "## Clean-slate answer", "## Background", 1), "missing section: clean-slate answer"},
		{"clean-slate empty", strings.Replace(validRecord, "From scratch I would not retry the token refresh at all: issue short-lived tokens and drop the refresh path.", "<!-- todo -->", 1), "section is empty: clean-slate answer"},
		{"no options section", strings.Replace(validRecord, "## Options", "## Ideas", 1), "missing section: options"},
		{"two options", strings.Replace(validRecord, "| 2 | Incremental: back off the retries | fewer 401s | S |\n", "", 1), "at least 3 options, found 2"},
		{"options as a list, not a table", strings.Replace(validRecord, "| 1 |", "- 1 |", 1), "found 2"},
		{"no status quo", strings.Replace(validRecord, "Status quo: keep retrying refresh", "Keep retrying refresh", 1), "no row names the status quo"},
		{"no radical", strings.Replace(validRecord, "Radical: short-lived", "Short-lived", 1), "no radical option"},
		{"the status quo row is not the radical one", strings.NewReplacer("Status quo: keep retrying refresh", "Status quo (radical to drop)", "Radical: short-lived", "Short-lived").Replace(validRecord), "no radical option"},
		{"constraints unclassified", strings.Replace(validRecord, "REAL", "hard", 1), "REAL and INERTIA"},
		{"constraints with only real", strings.Replace(validRecord, "INERTIA", "soft", 1), "REAL and INERTIA"},
		{"no choice", strings.Replace(validRecord, "## Choice and migration", "## Migration", 1), "missing section: choice"},
		{"no migration", strings.Replace(validRecord, "## Choice and migration", "## Choice", 1), "missing section: migration"},
	}
	for _, c := range cases {
		p := DecisionProblems(c.doc)
		found := false
		for _, line := range p {
			found = found || strings.Contains(line, c.expect)
		}
		if !found {
			t.Errorf("%s: problems %v lack %q", c.name, p, c.expect)
		}
	}
}

func TestDecisionPathKeyAndRead(t *testing.T) {
	top := t.TempDir()
	if got, want := DecisionPath(top, "plans/p1/phase-01"), filepath.Join(top, ".heavyq", "decisions", "plans", "p1", "phase-01.md"); got != want {
		t.Errorf("path = %s, want %s", got, want)
	}
	for _, c := range [][3]string{{"item-1", "x", "item-1"}, {"", "My Lane", "my-lane"}, {"", "///", "lane"}} {
		if got := DecisionKey(c[0], c[1]); got != c[2] {
			t.Errorf("DecisionKey(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
	if _, exists, err := ReadDecision(filepath.Join(top, "missing.md")); exists || err != nil {
		t.Errorf("missing: %v %v", exists, err)
	}
	p := DecisionPath(top, "x")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(validRecord), 0o644); err != nil {
		t.Fatal(err)
	}
	data, exists, err := ReadDecision(p)
	if err != nil || !exists || string(data) != validRecord {
		t.Errorf("read: exists %v err %v len %d", exists, err, len(data))
	}
	if err := os.WriteFile(p, make([]byte, maxDecisionBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadDecision(p); err == nil {
		t.Error("an oversized record must be refused")
	}
}

func TestCheckExit(t *testing.T) {
	bt := Stance{Name: "breakthrough", Exit: Exit{DecisionRecord: true, TimeboxMinutes: 30}}
	a := Active{Stance: "breakthrough", Since: t0, Expires: t0.Add(30 * time.Minute)}
	inside, past := t0.Add(10*time.Minute), t0.Add(31*time.Minute)

	if c := CheckExit(Stance{Name: "calm"}, Active{}, nil, false, inside); !c.Allowed || c.Reason != ExitManual {
		t.Errorf("no exit condition: %+v", c)
	}
	if c := CheckExit(bt, a, nil, false, inside); c.Allowed || len(c.Problems) != 1 || !strings.Contains(c.Problems[0], "no decision record") {
		t.Errorf("missing record: %+v", c)
	}
	if c := CheckExit(bt, a, []byte("# nothing"), true, inside); c.Allowed || len(c.Problems) < 3 {
		t.Errorf("incomplete record: %+v", c)
	}
	if c := CheckExit(bt, a, []byte(validRecord), true, inside); !c.Allowed || c.Reason != ExitDecisionRecord {
		t.Errorf("valid record: %+v", c)
	}
	if c := CheckExit(bt, a, nil, false, past); !c.Allowed || c.Reason != ExitTimebox {
		t.Errorf("past the time box: %+v", c)
	}
}
