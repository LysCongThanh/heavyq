package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
)

// Agent mode on the other surfaces: how it is switched, the JSON digest, the
// --digest flag, usage errors, why, the help card and the gate commands.

func TestOutputModeOverrides(t *testing.T) {
	for _, c := range []struct {
		name  string
		env   []string
		agent bool
	}{
		{"nothing", nil, false},
		{"claude code", []string{"CLAUDECODE=1"}, true},
		{"AI_AGENT", []string{"AI_AGENT=claude-code_2-1-316_agent"}, true},
		{"gemini", []string{"GEMINI_CLI=1"}, true},
		{"copilot", []string{"COPILOT_CLI=1"}, true},
		{"codex", []string{"CODEX_SANDBOX=seatbelt"}, true},
		{"cursor", []string{"CURSOR_AGENT=1"}, true},
		{"HEAVYQ_AGENT=1", []string{"HEAVYQ_AGENT=1"}, true},
		{"HEAVYQ_OUTPUT=agent", []string{"HEAVYQ_OUTPUT=agent"}, true},
		{"HEAVYQ_AGENT=0 forces off", []string{"CLAUDECODE=1", "HEAVYQ_AGENT=0"}, false},
		{"HEAVYQ_OUTPUT=human forces off", []string{"CLAUDECODE=1", "HEAVYQ_OUTPUT=human"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRig(t, baseConfig)
			h.env = append(h.env, c.env...)
			rc, out := h.run("3", "--", "true")
			if rc != 0 {
				t.Fatalf("rc=%d\n%s", rc, out)
			}
			agent := strings.Contains(out, "heavyq: running true") && strings.Contains(out, "heavyq: PASS true")
			human := strings.Contains(out, "heavyq: our turn after") && !strings.Contains(out, "PASS")
			if (c.agent && !agent) || (!c.agent && !human) {
				t.Fatalf("want agent=%v:\n%s", c.agent, out)
			}
		})
	}
}

func TestBadOutputModeIsReportedOnceAndIgnored(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "HEAVYQ_OUTPUT=xml", "CLAUDECODE=1")
	rc, out := h.run("3", "--", "true")
	const warn = `heavyq: warning: ignoring $HEAVYQ_OUTPUT="xml" (want agent, human or json)`
	if rc != 0 || strings.Count(out, warn) != 1 || !strings.Contains(out, "heavyq: PASS true") {
		t.Fatalf("rc=%d (a bad value is a warning, then the next rule decides):\n%s", rc, out)
	}
}

func TestJSONOutputModeEndsWithOneJSONLine(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "HEAVYQ_OUTPUT=json")
	rc, out := h.run("3", "--lane", "j", "--", "sh", "-c", "echo 'x_test.go:2: bad'; exit 4")
	if rc != 4 {
		t.Fatalf("rc=%d", rc)
	}
	var d struct {
		Heavyq, Verdict, Cmd, Log, Next string
		Exit                            int
		Failures                        []string
	}
	if err := json.Unmarshal([]byte(lastLine(out)), &d); err != nil {
		t.Fatalf("last line is not JSON: %v\n%s", err, out)
	}
	if d.Heavyq != "digest" || d.Verdict != "FAIL" || d.Exit != 4 || d.Cmd != "sh -c echo 'x_test.go:2: bad'; exit 4" ||
		len(d.Failures) != 1 || d.Failures[0] != "x_test.go:2: bad" || !strings.HasPrefix(d.Next, "fix the failure") {
		t.Fatalf("digest = %+v", d)
	}
	if _, err := os.Stat(d.Log); err != nil {
		t.Fatalf("log: %v", err)
	}
	if strings.Contains(out, "heavyq: FAIL") || !strings.Contains(out, "heavyq: running sh -c") {
		t.Fatalf("json mode: terse chatter, JSON digest:\n%s", out)
	}
}

func TestHumanDigestFlagAddsTheDigestAndKeepsTheChatter(t *testing.T) {
	h := newRig(t, baseConfig)
	args := []string{"3", "--lane", "d", "--", "sh", "-c", "echo oops; echo 'x_test.go:3: bad'; exit 1"}
	rc, out := h.run(args...)
	if rc != 1 || strings.Contains(out, "heavyq: FAIL") || strings.Contains(out, "next:") {
		t.Fatalf("a person gets no digest unless asked (rc=%d):\n%s", rc, out)
	}
	rc, out = h.run(append([]string{"3", "--digest"}, args[1:]...)...)
	d := digestOf(t, out)
	if rc != 1 || !strings.Contains(out, "heavyq: our turn after") || !strings.Contains(d, "  x_test.go:3: bad\n") ||
		!strings.HasPrefix(lastLine(d), "next: ") {
		t.Fatalf("rc=%d:\n%s", rc, out)
	}
}

func TestAgentModeUsageErrorsSayWhatToDo(t *testing.T) {
	h := newAgentRig(t)
	rc, out := h.run("3", "true") // no "--"
	if rc != ExitUsage || !strings.Contains(out, "heavyq: unknown flag") || lastLine(out) != "next: fix the arguments; `heavyq help agents` shows the usage" {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	h2 := newRig(t, baseConfig)
	if _, out := h2.run("3", "true"); strings.Contains(out, "next:") {
		t.Fatalf("a person gets no next: line:\n%s", out)
	}
}

func TestWhyStaysUnderTheByteBudgetInAgentMode(t *testing.T) {
	h := newAgentRig(t)
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	for i := 0; i < 12; i++ { // one lane with a long queue: the worst case for `why <lane>`
		h.start("5", "--lane", "big", "--", "sh", "-c", fmt.Sprintf("echo %d %s", i, strings.Repeat("padding ", 20)))
	}
	h.start("3", "--lane", "other", "--", "true")
	h.waiting(13)

	budget := func(name string, args ...string) {
		t.Helper()
		rc, out := h.run(args...)
		noANSI(t, out)
		if rc != 0 || len(out) > agentmode.WhyMaxBytes {
			t.Fatalf("%s: rc=%d, %d bytes, budget %d:\n%s", name, rc, len(out), agentmode.WhyMaxBytes, out)
		}
	}
	budget("why", "why")
	budget("why other", "why", "other")
	budget("why big", "why", "big")
	_, out := h.run("why", "big")
	if !strings.HasSuffix(out, "... (heavyq status shows the rest)\n") {
		t.Fatalf("a cut answer says so:\n%s", out)
	}
	// A person still gets all of it.
	if _, full := h.human().run("why", "big"); len(full) <= agentmode.WhyMaxBytes {
		t.Fatalf("test is vacuous: the human answer is only %d bytes", len(full))
	}
	h.touch("go")
	hold.wait(t, 60*time.Second)
}

func TestHelpAgentsCardFitsItsBudget(t *testing.T) {
	h := newRig(t, "")
	rc, out := h.run("help", "agents")
	if rc != 0 || len(out) > agentmode.CardMaxBytes || out != agentmode.Card {
		t.Fatalf("rc=%d, %d bytes (budget %d):\n%s", rc, len(out), agentmode.CardMaxBytes, out)
	}
	noANSI(t, out)
	if rc, out := h.run("help", "nonsense"); rc != ExitUsage || !strings.Contains(out, "no help topic") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if rc, out := h.run("help"); rc != 0 || !strings.Contains(out, "heavyq help [agents]") {
		t.Fatalf("plain help rc=%d\n%s", rc, out)
	}
}

func TestAgentModeGateStatusAndRunEndWithNext(t *testing.T) {
	h := newAgentRig(t)
	repo := gateRepo(t, h, "")
	rc, out := h.run("gate", "status")
	if rc != 1 || lastLine(out) != "next: heavyq gate run ready" || strings.Contains(out, "Prove it with") {
		t.Fatalf("status rc=%d\n%s", rc, out)
	}
	if _, out := h.human().run("gate", "status"); !strings.Contains(out, "Prove it with: heavyq gate run ready") || strings.Contains(out, "next:") {
		t.Fatalf("a person keeps the sentence:\n%s", out)
	}

	h.edit(repo, "broken\n")
	rc, out = h.run("gate", "run", "ready")
	noANSI(t, out)
	if rc != 1 || lastLine(out) != "next: heavyq gate run ready" || !strings.Contains(out, "heavyq: FAIL ") {
		t.Fatalf("failing gate run rc=%d\n%s", rc, out)
	}
	h.edit(repo, "from-commit\n")
	rc, out = h.run("gate", "run", "ready")
	if rc != 0 || strings.Contains(out, "next:") || !strings.Contains(out, "PROVEN") {
		t.Fatalf("proving run rc=%d\n%s", rc, out)
	}
}
